package handlers

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/middleware"
)

// seedDriverForShare links an auth user to a driver row inside tenant 1.
func seedDriverForShare(t *testing.T, db *sql.DB, id, code, email string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO users (id, email, password_hash, name, role_id, status, tenant_id)
		VALUES (?, ?, 'hash', 'Driver', 5, 'active', '1')`, id, email)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO drivers (id, driver_id, first_name, last_name, phone, email,
			license_number, license_expiry, status, tenant_id)
		VALUES (?, ?, 'First', 'Last', '+919000000000', ?, 'DL-1', '2030-01-01', 'available', '1')`,
		id, code, email)
	require.NoError(t, err)
}

// shareDriverRouter mounts the driver share route exactly as main.go does and
// seeds three trips: one assigned to the caller, one to another driver, one
// with no driver at all.
func shareDriverRouter(t *testing.T) (*App, *chi.Mux, *sql.DB) {
	t.Helper()
	db := newShareTestDB(t)
	app := newShareTestApp(t, db, allowAuthSvc{})

	_, err := db.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare)
		VALUES ('r-share-drv', 'Mumbai', 'Pune', 150.0, 3.5, 5000.0)`)
	require.NoError(t, err)

	seedDriverForShare(t, db, "drv-mine", "DRV-MINE", "mine@t.example")
	seedDriverForShare(t, db, "drv-other", "DRV-OTHER", "other@t.example")

	for _, tc := range []struct{ id, driverID string }{
		{"trip-mine", "drv-mine"},
		{"trip-other", "drv-other"},
		{"trip-unassigned", ""},
	} {
		_, err := db.Exec(`INSERT INTO trips
			(id, trip_number, route_id, driver_id, departure_time, arrival_time, status, tenant_id)
			VALUES (?, ?, 'r-share-drv', ?, '2026-08-19 08:00:00', '2026-08-19 14:00:00', 'in_transit', '1')`,
			tc.id, "TRP-"+tc.id, tc.driverID)
		require.NoError(t, err)
	}

	r := chi.NewRouter()
	r.With(
		withUserAndTenant("drv-mine", "1", nil),
		middleware.RequirePermission(app.AuthSrv, "driver", "write-self"),
	).Post("/api/v1/drivers/me/trips/{id}/share", app.Share.ShareMyTrip)
	return app, r, db
}

func shareLinkCount(t *testing.T, db *sql.DB, tripID string) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM share_links WHERE trip_id = ?`, tripID).Scan(&n))
	return n
}

// The driver's own trip mints a real link (201 + /share/<token>).
func TestShare_MyTrip_MintsForAssignedTrip(t *testing.T) {
	_, r, db := shareDriverRouter(t)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/drivers/me/trips/trip-mine/share", nil))

	require.Equal(t, http.StatusCreated, w.Code)
	assert.Contains(t, w.Body.String(), "/share/", "response must carry the share URL the app hands to the customer")
	assert.Equal(t, 1, shareLinkCount(t, db, "trip-mine"))
}

// Same tenant, different assigned driver: 403, and no link row is written.
// Ratchet: fails if ShareMyTrip skips the ownership gate and delegates blindly.
func TestShare_MyTrip_ForbiddenForAnotherDriversTrip(t *testing.T) {
	_, r, db := shareDriverRouter(t)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/drivers/me/trips/trip-other/share", nil))

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, 0, shareLinkCount(t, db, "trip-other"), "no link may be minted for a trip the caller does not drive")
}

// An unassigned trip is nobody's trip — not mintable.
func TestShare_MyTrip_ForbiddenWhenUnassigned(t *testing.T) {
	_, r, db := shareDriverRouter(t)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/drivers/me/trips/trip-unassigned/share", nil))

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, 0, shareLinkCount(t, db, "trip-unassigned"))
}
