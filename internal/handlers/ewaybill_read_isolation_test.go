package handlers

// Regression test for the 2026-11-11 security fix: the E-Way Bill
// presentation pages are tenant-scoped via the owning trip and fail closed on
// an empty tenant context. Cross-tenant operators must never see another
// tenant's EWB numbers; previously the list/stats views were global.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
	"transport-app/internal/ewaybill"
	"transport-app/internal/shared"
)

// newEWBIsolationTestApp seeds two tenants with one EWB each plus the shared
// templates/handler wiring used by the presentation pages.
func newEWBIsolationTestApp(t *testing.T) (*App, *EWayBillHandlers) {
	t.Helper()
	db := handlerTestDB(t)
	tmpl, err := parseTemplates(&mockAuthSvc{})
	require.NoError(t, err)
	app := &App{DB: db, Templates: tmpl, AuthSrv: &mockAuthSvc{}}
	svc := ewaybill.NewEWayBillService(db, nil, nil, nil, ewaybill.Config{})
	h := NewEWayBillHandlers(app, svc, &mockAuthSvc{})

	_, err = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES
		('tenant-a', 'Tenant A', 'tenant-a'), ('tenant-b', 'Tenant B', 'tenant-b')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO routes (id, tenant_id, source, destination, distance, estimated_hours, standard_fare) VALUES
		('route-a', 'tenant-a', 'Mumbai', 'Pune', 150, 3, 100), ('route-b', 'tenant-b', 'Delhi', 'Jaipur', 280, 5, 200)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO trips (id, trip_number, tenant_id, route_id, status, departure_time) VALUES
		('trip-a', 'TRP-A', 'tenant-a', 'route-a', 'in_transit', '2026-09-01 08:00:00'), ('trip-b', 'TRP-B', 'tenant-b', 'route-b', 'in_transit', '2026-09-02 08:00:00')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO eway_bills (id, trip_id, ewb_number, generation_date, valid_until, status, goods_value, distance) VALUES
		('eb-a', 'trip-a', 'EWB-A1', '2026-09-01 00:00:00', '2026-12-31 00:00:00', 'active', 60000, 150),
		('eb-b', 'trip-b', 'EWB-B1', '2026-09-02 00:00:00', '2026-12-31 00:00:00', 'active', 75000, 150)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO eway_bill_events (id, ewb_number, trip_id, event_type, payload, created_by, created_at) VALUES
		('ev-a', 'EWB-A1', 'trip-a', 'PART_A_GENERATED', '{"ok":true}', 'u-a', '2026-09-01 09:00:00'),
		('ev-b', 'EWB-B1', 'trip-b', 'PART_A_GENERATED', '{}', 'u-b', '2026-09-02 09:00:00')`)
	require.NoError(t, err)
	// Sanity: the read model sees the seeded rows for the right tenant.
	items := ewaybill.NewEWayBillReadQueries(app.DB).Items(context.Background(), "tenant-a")
	require.Len(t, items, 1, "fixture sanity: tenant-a EWB visible to the read model")
	return app, h
}

func ewbIsolationRequest(path, tenant string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	ctx := req.Context()
	if tenant != "" {
		ctx = shared.ContextWithTenantID(ctx, shared.TenantID(tenant))
	}
	ctx = context.WithValue(ctx, auth.ContextUser, &auth.SessionData{UserID: "u-admin", Role: "admin"})
	return req.WithContext(ctx)
}

func TestEWayBillList_TenantScopedIsolation(t *testing.T) {
	_, h := newEWBIsolationTestApp(t)

	for _, tc := range []struct {
		tenant  string
		want    string
		notWant string
	}{
		{"tenant-a", "EWB-A1", "EWB-B1"},
		{"tenant-b", "EWB-B1", "EWB-A1"},
	} {
		w := httptest.NewRecorder()
		h.List(w, ewbIsolationRequest("/ewaybill", tc.tenant))
		require.Equal(t, http.StatusOK, w.Code)
		body := w.Body.String()
		assert.Contains(t, body, tc.want, "own tenant's EWB is listed")
		assert.NotContains(t, body, tc.notWant, "other tenant's EWB must never be listed")
		// Candidate-trip dropdown is tenant-scoped too (same list page).
		ownTrip := "TRP-A"
		otherTrip := "TRP-B"
		if tc.tenant == "tenant-b" {
			ownTrip, otherTrip = "TRP-B", "TRP-A"
		}
		assert.Contains(t, body, ownTrip, "own tenant candidate trip is offered")
		assert.NotContains(t, body, otherTrip, "other tenant candidate trips must never be offered")
	}

	// Fail closed: a missing tenant context must render no EWBs at all.
	w := httptest.NewRecorder()
	h.List(w, ewbIsolationRequest("/ewaybill", ""))
	require.Equal(t, http.StatusOK, w.Code)
	assert.NotContains(t, w.Body.String(), "EWB-A1")
	assert.NotContains(t, w.Body.String(), "EWB-B1")
	assert.NotContains(t, w.Body.String(), "TRP-A")
	assert.NotContains(t, w.Body.String(), "TRP-B")
}

// TestEWayBillDetail_TenantScopedIsolation proves the detail record lookup is
// tenant-scoped too: tenant A requesting tenant B's EWB gets the SAME 404 body
// as an unknown number (no existence oracle), while its own record renders.
func TestEWayBillDetail_TenantScopedIsolation(t *testing.T) {
	_, h := newEWBIsolationTestApp(t)
	router := chi.NewRouter()
	router.Get("/ewaybill/{ewb}", h.Detail)

	own := httptest.NewRecorder()
	ownReq := ewbIsolationRequest("/ewaybill/EWB-A1", "tenant-a")
	ownReq.Header.Set("Accept", "application/json")
	router.ServeHTTP(own, ownReq)
	require.Equal(t, http.StatusOK, own.Code)
	ownBody := own.Body.String()
	assert.Contains(t, ownBody, "EWB-A1")
	assert.Contains(t, ownBody, "PART_A_GENERATED", "own tenant event history is included")
	assert.NotContains(t, ownBody, "EWB-B1")

	cross := httptest.NewRecorder()
	crossReq := ewbIsolationRequest("/ewaybill/EWB-B1", "tenant-a")
	crossReq.Header.Set("Accept", "application/json")
	router.ServeHTTP(cross, crossReq)
	require.Equal(t, http.StatusNotFound, cross.Code)

	unknown := httptest.NewRecorder()
	unknownReq := ewbIsolationRequest("/ewaybill/EWB-UNKNOWN", "tenant-a")
	unknownReq.Header.Set("Accept", "application/json")
	router.ServeHTTP(unknown, unknownReq)
	require.Equal(t, http.StatusNotFound, unknown.Code)
	assert.Equal(t, cross.Body.String(), unknown.Body.String(), "cross-tenant must be indistinguishable from unknown (no existence oracle)")
	assert.Contains(t, cross.Body.String(), "E-Way Bill not found")

	empty := httptest.NewRecorder()
	emptyReq := ewbIsolationRequest("/ewaybill/EWB-A1", "")
	emptyReq.Header.Set("Accept", "application/json")
	router.ServeHTTP(empty, emptyReq)
	require.Equal(t, http.StatusNotFound, empty.Code, "empty tenant fails closed")
}

// TestEWayBillTripCard_TenantScopedIsolation covers the trip-page EWB card
// read: tenant A requesting tenant B's trip gets the same 404 as an unknown
// trip; its own trip still renders the record.
func TestEWayBillTripCard_TenantScopedIsolation(t *testing.T) {
	_, h := newEWBIsolationTestApp(t)
	router := chi.NewRouter()
	router.Get("/trips/{id}/ewaybill", h.GetForTrip)

	own := httptest.NewRecorder()
	ownReq := ewbIsolationRequest("/trips/trip-a/ewaybill", "tenant-a")
	ownReq.Header.Set("Accept", "application/json")
	router.ServeHTTP(own, ownReq)
	require.Equal(t, http.StatusOK, own.Code, "body: "+own.Body.String())
	assert.Contains(t, own.Body.String(), "EWB-A1")

	cross := httptest.NewRecorder()
	crossReq := ewbIsolationRequest("/trips/trip-b/ewaybill", "tenant-a")
	crossReq.Header.Set("Accept", "application/json")
	router.ServeHTTP(cross, crossReq)
	require.Equal(t, http.StatusNotFound, cross.Code)

	unknown := httptest.NewRecorder()
	unknownReq := ewbIsolationRequest("/trips/trip-unknown/ewaybill", "tenant-a")
	unknownReq.Header.Set("Accept", "application/json")
	router.ServeHTTP(unknown, unknownReq)
	require.Equal(t, http.StatusNotFound, unknown.Code)
	assert.Equal(t, cross.Body.String(), unknown.Body.String(), "cross-tenant trip == unknown trip")
	assert.Contains(t, cross.Body.String(), "no e-way bill found for trip")
}
