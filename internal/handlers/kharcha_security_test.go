package handlers

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
)

// seedKharchaSecurityFixture creates two drivers, their users, and one trip
// per driver. The acting driver is u-drv-1 / d-1.
func seedKharchaSecurityFixture(t *testing.T, dbConn *sql.DB) {
	t.Helper()
	_, err := dbConn.Exec(`
		INSERT INTO users (id, name, email, password_hash, role_id, status, tenant_id)
		VALUES ('u-drv-1','Driver One','drv1@test.local','hash',5,'active','1'),
		       ('u-drv-2','Driver Two','drv2@test.local','hash',5,'active','1');

		INSERT INTO drivers
			(id, driver_id, first_name, last_name, phone, email, license_number, license_expiry, status, tenant_id)
		VALUES ('d-1','DRV-001','Driver','One','+919000000001','drv1@test.local','DL-1','2030-01-01','available','1'),
		       ('d-2','DRV-002','Driver','Two','+919000000002','drv2@test.local','DL-2','2030-01-01','available','1');

		INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare, tenant_id)
		VALUES ('r-kh','Mumbai','Pune',150,3.5,5000,'1');

		INSERT INTO trips (id, trip_number, route_id, driver_id, departure_time, status, tenant_id)
		VALUES ('t-own','TRP-KH-1','r-kh','d-1','2026-11-16 08:00:00','in_transit','1'),
		       ('t-other','TRP-KH-2','r-kh','d-2','2026-11-16 09:00:00','in_transit','1');
	`)
	require.NoError(t, err)
}

func postKharchaCreate(t *testing.T, r http.Handler, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/kharcha/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func kharchaCount(t *testing.T, dbConn *sql.DB) int {
	t.Helper()
	var n int
	require.NoError(t, dbConn.QueryRow(`SELECT COUNT(*) FROM driver_expenses`).Scan(&n))
	return n
}

// TestKharchaCreate_DriverCannotFileForAnotherDriver proves the web form's
// free-form driver_id is not an authorization bypass: the driver role can only
// file for the driver row linked to its own account.
func TestKharchaCreate_DriverCannotFileForAnotherDriver(t *testing.T) {
	dbConn, app, tripAPI := setupMobileAPITestEnv(t)
	defer dbConn.Close()
	seedKharchaSecurityFixture(t, dbConn)

	user := &auth.SessionData{UserID: "u-drv-1", Role: "driver"}
	r := buildTestRouterWithAuth(app, tripAPI, user)
	r.Post("/kharcha/create", app.Kharcha.Create)

	w := postKharchaCreate(t, r, url.Values{
		"trip_id":   {"t-own"},
		"driver_id": {"d-2"},
		"category":  {"toll"},
		"amount":    {"100"},
	})
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, 0, kharchaCount(t, dbConn), "no claim may be written for another driver")
}

// TestKharchaCreate_DriverCannotFileForAnotherDriversTrip proves a driver
// cannot attach a claim to a trip they do not drive (the settlement flow keys
// deductions by trip+driver, so a foreign trip would create an unbacked claim).
func TestKharchaCreate_DriverCannotFileForAnotherDriversTrip(t *testing.T) {
	dbConn, app, tripAPI := setupMobileAPITestEnv(t)
	defer dbConn.Close()
	seedKharchaSecurityFixture(t, dbConn)

	user := &auth.SessionData{UserID: "u-drv-1", Role: "driver"}
	r := buildTestRouterWithAuth(app, tripAPI, user)
	r.Post("/kharcha/create", app.Kharcha.Create)

	w := postKharchaCreate(t, r, url.Values{
		"trip_id":  {"t-other"},
		"category": {"toll"},
		"amount":   {"100"},
	})
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, 0, kharchaCount(t, dbConn), "no claim may be written against another driver's trip")
}

// TestKharchaCreate_DriverFilesOwnExpense is the positive control: an empty
// driver_id is filled from the authenticated driver's linked driver row.
func TestKharchaCreate_DriverFilesOwnExpense(t *testing.T) {
	dbConn, app, tripAPI := setupMobileAPITestEnv(t)
	defer dbConn.Close()
	seedKharchaSecurityFixture(t, dbConn)

	user := &auth.SessionData{UserID: "u-drv-1", Role: "driver"}
	r := buildTestRouterWithAuth(app, tripAPI, user)
	r.Post("/kharcha/create", app.Kharcha.Create)

	w := postKharchaCreate(t, r, url.Values{
		"trip_id":  {"t-own"},
		"category": {"toll"},
		"amount":   {"100"},
	})
	assert.Equal(t, http.StatusSeeOther, w.Code)
	require.Equal(t, 1, kharchaCount(t, dbConn))

	var driverID, tenantID string
	require.NoError(t, dbConn.QueryRow(`SELECT driver_id, tenant_id FROM driver_expenses LIMIT 1`).Scan(&driverID, &tenantID))
	assert.Equal(t, "d-1", driverID, "claim must be attributed to the acting driver")
	assert.Equal(t, "1", tenantID)
}
