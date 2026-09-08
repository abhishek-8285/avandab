package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
	"transport-app/internal/shared"
	tripagg "transport-app/internal/trip/domain/aggregate"
	triprepo "transport-app/internal/trip/infrastructure/persistence/sql"
)

// TestTripStopAPI_JSONContract locks the Bearer-API shape of the multistop
// endpoints the mobile app posts to (offline queue + POD screen):
// /api/v1/trips/{id}/stops/{stopId}/{reach,pod,complete} must answer JSON,
// never the web 303 redirect. The redirect class of failure is silent data
// loss: fetch follows the 303 to a 200 page and the queue clears a POD the
// server never received.
func TestTripStopAPI_JSONContract(t *testing.T) {
	db := handlerTestDB(t)
	h := &TripHandlers{App: &App{DB: db}}

	r := chi.NewRouter()
	r.Post("/api/v1/trips/{id}/stops/{stopId}/reach", h.ReachStop)
	r.Post("/api/v1/trips/{id}/stops/{stopId}/pod", h.SubmitStopPOD)
	r.Post("/api/v1/trips/{id}/stops/{stopId}/complete", h.CompleteStop)

	tenantID := shared.TenantID("1")
	_, err := db.Exec(`INSERT INTO customers (id, name, company, phone, tenant_id) VALUES ('cust_api', 'API Customer', 'API Logistics', '9988776655', '1')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare, tenant_id) VALUES ('r_api', 'Delhi', 'Jaipur', 250, 5, 12000, '1')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO drivers (id, driver_id, first_name, last_name, phone, license_number, license_expiry, status, tenant_id) VALUES ('d-api', 'DRV-API', 'Api', 'Driver', '9876543210', 'DL-API-1', date('now','+1 year'), 'available', '1')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, status, insurance_expiry, fitness_expiry, permit_expiry, tenant_id) VALUES ('v-api', 'DL-01-A-0001', 'DL-01-A-0001', 'truck', 20, 'available', date('now','+1 year'), date('now','+1 year'), date('now','+1 year'), '1')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO bookings (id, booking_number, customer_id, pickup_date, route_id, vehicle_type, price, status, tenant_id) VALUES ('bk_api', 'BK-API-01', 'cust_api', '2026-08-30 09:00:00', 'r_api', 'truck', 12000, 'confirmed', '1')`)
	require.NoError(t, err)

	tripID := tripagg.TripID("trip_api_1")
	bookingID := "bk_api"
	trip := tripagg.NewTripAggregate(tripID, tenantID, "TRIP-API-001", &bookingID, "r_api", time.Now().UTC(), "Delhi -> Jaipur", time.Now().UTC())
	require.NoError(t, trip.Schedule(time.Now().UTC()))
	require.NoError(t, trip.AssignDriver("d-api", time.Now().UTC()))
	require.NoError(t, trip.AssignVehicle("v-api", time.Now().UTC()))
	require.NoError(t, trip.Start(time.Now().UTC()))
	trip.AddStop(tripagg.TripStop{
		ID:           "stop_api_1",
		TenantID:     tenantID,
		TripID:       tripID,
		StopSequence: 1,
		StopType:     tripagg.StopTypeDrop,
		LocationName: "Jaipur Depo",
		PODRequired:  false,
		Status:       tripagg.StopStatusPending,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	})
	require.NoError(t, triprepo.NewTripRepository(db).Save(context.Background(), trip))

	postBody := func(path string, body []byte, contentType string) *httptest.ResponseRecorder {
		var req *http.Request
		if body != nil {
			req = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
			req.Header.Set("Content-Type", contentType)
		} else {
			req = httptest.NewRequest(http.MethodPost, path, nil)
		}
		ctx := shared.ContextWithTenantID(req.Context(), tenantID)
		ctx = context.WithValue(ctx, auth.ContextUser, &auth.SessionData{UserID: "d-api", Role: "driver"})
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req.WithContext(ctx))
		return rr
	}
	post := func(path string) *httptest.ResponseRecorder {
		return postBody(path, nil, "")
	}
	assertJSON := func(rr *httptest.ResponseRecorder) {
		assert.Contains(t, rr.Header().Get("Content-Type"), "application/json", "API stop endpoints must answer JSON, never a web redirect")
	}

	rr := post("/api/v1/trips/trip_api_1/stops/stop_api_1/reach")
	require.Equal(t, http.StatusOK, rr.Code)
	assertJSON(rr)
	var reached map[string]string
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &reached))
	assert.Equal(t, "arrived", reached["status"])

	rr = postBody("/api/v1/trips/trip_api_1/stops/stop_api_1/pod",
		[]byte(`{"pod_url":"http://example.com/pod.jpg","notes":"left at gate"}`), "application/json")
	require.Equal(t, http.StatusOK, rr.Code)
	assertJSON(rr)
	var podded map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &podded))
	assert.Equal(t, "pod_verified", podded["status"])

	rr = post("/api/v1/trips/trip_api_1/stops/stop_api_1/complete")
	require.Equal(t, http.StatusOK, rr.Code)
	assertJSON(rr)
	var completed map[string]string
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &completed))
	assert.Equal(t, "completed", completed["status"])

	// Unknown trip: 400 fail-closed (plain-text error envelope, which the
	// mobile clients tolerate via .json().catch() — the contract that
	// matters is success-JSON above, never a redirect).
	rr = post("/api/v1/trips/nope/stops/nope/reach")
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.NotEmpty(t, rr.Body.String())
}
