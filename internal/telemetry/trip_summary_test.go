package telemetry

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"transport-app/internal/shared"
)

func TestTripSummaryHandler_FullJoin(t *testing.T) {
	db := newTestIngestorDB(t)
	insertTestRoute(t, db, "r-sum")
	insertTestDriver(t, db, "d-sum", "Abhishek", "Shrivastav", "+91-98765-43210")
	insertTestVehicleReg(t, db, "v-sum", "REG-SUM")

	_, err := db.Exec(`INSERT INTO customers (id, name, phone) VALUES ('c-sum', 'Acme', '9999999999')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO bookings (id, booking_number, customer_id, pickup_date, route_id, vehicle_type, cargo_weight, price, status)
		VALUES ('b-sum', 'BK-sum', 'c-sum', datetime('now'), 'r-sum', 'truck', 12400, 5000, 'confirmed')`)
	require.NoError(t, err)

	dep := time.Now().UTC().Add(-2 * time.Hour).Format("2006-01-02 15:04:05")
	started := time.Now().UTC().Add(-90 * time.Minute).Format("2006-01-02 15:04:05")
	_, err = db.Exec(`INSERT INTO trips (id, trip_number, booking_id, driver_id, vehicle_id, route_id, departure_time, status, started_at, in_transit_at, tenant_id)
		VALUES ('t-sum', 'TRP-SUM-1', 'b-sum', 'd-sum', 'v-sum', 'r-sum', ?, 'in_transit', ?, ?, '1')`,
		dep, started, started)
	require.NoError(t, err)

	// Update route with realistic names (insertTestRoute hardcodes src/dst).
	_, err = db.Exec(`UPDATE routes SET source = 'Delhi', destination = 'Gurgaon', distance = 45, estimated_hours = 1.5 WHERE id = 'r-sum'`)
	require.NoError(t, err)

	srv := chi.NewRouter()
	srv.Get("/api/v1/trips/{id}/summary", TripSummaryHandler(db))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/trips/t-sum/summary", nil)
	req = req.WithContext(shared.ContextWithTenantID(req.Context(), "1"))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var s TripSummary
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &s))
	assert.Equal(t, "t-sum", s.TripID)
	assert.Equal(t, "TRP-SUM-1", s.TripNumber)
	assert.Equal(t, "in_transit", s.Status)
	assert.Equal(t, "Delhi", s.Origin)
	assert.Equal(t, "Gurgaon", s.Destination)
	require.NotNil(t, s.RouteKM)
	assert.InDelta(t, 45, *s.RouteKM, 0.001)
	require.NotNil(t, s.EstHours)
	assert.InDelta(t, 1.5, *s.EstHours, 0.001)
	require.NotNil(t, s.CargoWeightKG)
	assert.InDelta(t, 12400, *s.CargoWeightKG, 0.001)
	assert.Equal(t, "MH-01-REG-SUM", s.VehicleNumber)
	assert.Equal(t, "Abhishek Shrivastav", s.DriverName)
	assert.Equal(t, "+91-98765-43210", s.DriverPhone)
	require.NotNil(t, s.StartedAt)
	require.NotNil(t, s.InTransitAt)
	assert.Nil(t, s.DeliveredAt)
	assert.Nil(t, s.CompletedAt)
}

func TestTripSummaryHandler_MinimalTrip(t *testing.T) {
	db := newTestIngestorDB(t)
	insertTestRoute(t, db, "r-min")
	_, err := db.Exec(`INSERT INTO trips (id, trip_number, route_id, departure_time, status, tenant_id)
		VALUES ('t-min', 'TRP-MIN', 'r-min', datetime('now'), 'scheduled', '1')`)
	require.NoError(t, err)

	srv := chi.NewRouter()
	srv.Get("/api/v1/trips/{id}/summary", TripSummaryHandler(db))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/trips/t-min/summary", nil)
	req = req.WithContext(shared.ContextWithTenantID(req.Context(), "1"))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var s TripSummary
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &s))
	assert.Equal(t, "t-min", s.TripID)
	assert.Equal(t, "scheduled", s.Status)
	assert.Empty(t, s.DriverName)
	assert.Nil(t, s.CargoWeightKG)
}

func TestTripSummaryHandler_TenantScoped(t *testing.T) {
	db := newTestIngestorDB(t)
	insertTestRoute(t, db, "r-ten")
	_, err := db.Exec(`INSERT INTO trips (id, trip_number, route_id, departure_time, status, tenant_id)
		VALUES ('t-ten', 'TRP-TEN', 'r-ten', datetime('now'), 'draft', 'other-tenant')`)
	require.NoError(t, err)

	srv := chi.NewRouter()
	srv.Get("/api/v1/trips/{id}/summary", TripSummaryHandler(db))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/trips/t-ten/summary", nil)
	req = req.WithContext(shared.ContextWithTenantID(req.Context(), "1"))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	// Default tenant caller must NOT see another tenant's trip.
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestTripSummaryHandler_NotFound(t *testing.T) {
	db := newTestIngestorDB(t)
	srv := chi.NewRouter()
	srv.Get("/api/v1/trips/{id}/summary", TripSummaryHandler(db))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/trips/nope/summary", nil)
	req = req.WithContext(shared.ContextWithTenantID(req.Context(), "1"))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}
