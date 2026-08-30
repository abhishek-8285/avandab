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

	"transport-app/internal/shared"
	tripagg "transport-app/internal/trip/domain/aggregate"
	triprepo "transport-app/internal/trip/infrastructure/persistence/sql"
)

func stopRequest(method, path, tripID, stopID string, body []byte, tenant shared.TenantID) *http.Request {
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.Header.Set("Accept", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", tripID)
	if stopID != "" {
		rctx.URLParams.Add("stopId", stopID)
	}
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = shared.ContextWithTenantID(ctx, tenant)
	return req.WithContext(ctx)
}

func TestMultiStopTrip_E2E_HTTPAndIntegrations(t *testing.T) {
	db := handlerTestDB(t)
	tenantID := shared.TenantID("1")
	h := &TripHandlers{App: &App{DB: db}}

	// Seed customer, route, booking, driver, vehicle
	_, err := db.Exec(`INSERT INTO customers (id, name, company, phone, tenant_id) VALUES ('cust_multi', 'Multi Customer', 'Multi Logistics', '9988776655', '1')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare, tenant_id) VALUES ('r_multi', 'Delhi', 'Udaipur', 650, 12, 35000, '1')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO drivers (id, driver_id, first_name, last_name, phone, license_number, license_expiry, status, tenant_id) VALUES ('d-multi', 'DRV-MULTI', 'Vikram', 'Singh', '9876543210', 'DL-MULTI-1', date('now','+1 year'), 'available', '1')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, status, insurance_expiry, fitness_expiry, permit_expiry, tenant_id) VALUES ('v-multi', 'DL-01-M-1234', 'DL-01-M-1234', 'truck', 20, 'available', date('now','+1 year'), date('now','+1 year'), date('now','+1 year'), '1')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO bookings (id, booking_number, customer_id, pickup_date, route_id, vehicle_type, price, status, tenant_id) VALUES ('bk_multi', 'BK-MULTI-01', 'cust_multi', '2026-08-30 09:00:00', 'r_multi', 'truck', 35000, 'confirmed', '1')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO company_settings (id, company_name, currency, timezone, gst_enabled, gst_rate) VALUES (1, 'Avandab', 'INR', 'Asia/Kolkata', 1, 18) ON CONFLICT(id) DO UPDATE SET gst_enabled = 1, gst_rate = 18`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO geofences (id, tenant_id, name, kind, shape, center_lat, center_lng, radius_m, priority, is_active) VALUES ('g_multi', '1', 'Jaipur Hub', 'depot', 'circle', 26.9, 75.8, 500, 10, 1)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO trip_detentions (id, tenant_id, trip_id, vehicle_id, geofence_id, zone_kind, entered_at, exited_at, dwell_seconds, free_seconds, billable_seconds, rate_per_hour, amount, status) VALUES ('d_multi_1', '1', 'trip_multi_1', NULL, 'g_multi', 'pickup', '2026-08-30 10:00:00', '2026-08-30 12:00:00', 7200, 1800, 5400, 100, 150.0, 'closed')`)
	require.NoError(t, err)

	bookingID := "bk_multi"
	tripID := tripagg.TripID("trip_multi_1")
	trip := tripagg.NewTripAggregate(
		tripID,
		tenantID,
		"TRIP-MULTI-001",
		&bookingID,
		"r_multi",
		time.Now().UTC(),
		"Delhi -> Jaipur -> Udaipur Multi-Drop",
		time.Now().UTC(),
	)
	require.NoError(t, trip.Schedule(time.Now().UTC()))
	require.NoError(t, trip.AssignDriver("d-multi", time.Now().UTC()))
	require.NoError(t, trip.AssignVehicle("v-multi", time.Now().UTC()))
	require.NoError(t, trip.Start(time.Now().UTC()))

	stop1 := tripagg.TripStop{
		ID:           "stop_delhi",
		TenantID:     tenantID,
		TripID:       tripID,
		StopSequence: 1,
		StopType:     tripagg.StopTypePickup,
		LocationName: "Delhi Warehouse",
		Address:      "Okhla Phase III, New Delhi",
		OTPRequired:  false,
		PODRequired:  false,
		Status:       tripagg.StopStatusPending,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	stop2 := tripagg.TripStop{
		ID:             "stop_jaipur",
		TenantID:       tenantID,
		TripID:         tripID,
		StopSequence:   2,
		StopType:       tripagg.StopTypeDrop,
		LocationName:   "Jaipur Retail Depo",
		Address:        "MI Road, Jaipur",
		ConsigneeName:  "Jaipur Retailers",
		ConsigneePhone: "+91-9811111111",
		OTPRequired:    true,
		OTPCode:        "7890",
		PODRequired:    true,
		Status:         tripagg.StopStatusPending,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}
	stop3 := tripagg.TripStop{
		ID:             "stop_udaipur",
		TenantID:       tenantID,
		TripID:         tripID,
		StopSequence:   3,
		StopType:       tripagg.StopTypeDrop,
		LocationName:   "Udaipur Wholesale Terminal",
		Address:        "City Station Rd, Udaipur",
		ConsigneeName:  "Udaipur Spices",
		ConsigneePhone: "+91-9822222222",
		OTPRequired:    false,
		PODRequired:    true,
		Status:         tripagg.StopStatusPending,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}

	trip.AddStop(stop1)
	trip.AddStop(stop2)
	trip.AddStop(stop3)

	repo := triprepo.NewTripRepository(db)
	require.NoError(t, repo.Save(context.Background(), trip))

	// 1. HTTP Reach Stop 1
	w := httptest.NewRecorder()
	h.ReachStop(w, stopRequest("POST", "/trips/trip_multi_1/stops/stop_delhi/reach", "trip_multi_1", "stop_delhi", nil, tenantID))
	assert.Equal(t, http.StatusOK, w.Code)

	// Complete Stop 1
	w = httptest.NewRecorder()
	h.CompleteStop(w, stopRequest("POST", "/trips/trip_multi_1/stops/stop_delhi/complete", "trip_multi_1", "stop_delhi", nil, tenantID))
	assert.Equal(t, http.StatusOK, w.Code)

	// Transition trip to in_transit
	w = httptest.NewRecorder()
	h.ReachPickup(w, stopRequest("POST", "/trips/trip_multi_1/reach-pickup", "trip_multi_1", "", nil, tenantID))
	w = httptest.NewRecorder()
	h.StartTransit(w, stopRequest("POST", "/trips/trip_multi_1/in-transit", "trip_multi_1", "", nil, tenantID))

	// 2. HTTP Reach Stop 2
	w = httptest.NewRecorder()
	h.ReachStop(w, stopRequest("POST", "/trips/trip_multi_1/stops/stop_jaipur/reach", "trip_multi_1", "stop_jaipur", nil, tenantID))
	assert.Equal(t, http.StatusOK, w.Code)

	// Submit Stop 2 POD with invalid OTP -> Expect 400 Bad Request
	badOTPBody, _ := json.Marshal(map[string]string{
		"pod_url": "https://s3.example.com/pod_stop_2.jpg",
		"otp":     "0000",
	})
	w = httptest.NewRecorder()
	h.SubmitStopPOD(w, stopRequest("POST", "/trips/trip_multi_1/stops/stop_jaipur/pod", "trip_multi_1", "stop_jaipur", badOTPBody, tenantID))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Submit Stop 2 POD with correct OTP -> Expect 200 OK
	goodOTPBody, _ := json.Marshal(map[string]string{
		"pod_url":       "https://s3.example.com/pod_stop_2.jpg",
		"signature_url": "https://s3.example.com/sig_stop_2.png",
		"notes":         "20 cartons received in good shape",
		"otp":           "7890",
	})
	w = httptest.NewRecorder()
	h.SubmitStopPOD(w, stopRequest("POST", "/trips/trip_multi_1/stops/stop_jaipur/pod", "trip_multi_1", "stop_jaipur", goodOTPBody, tenantID))
	assert.Equal(t, http.StatusOK, w.Code)

	// Complete Stop 2
	w = httptest.NewRecorder()
	h.CompleteStop(w, stopRequest("POST", "/trips/trip_multi_1/stops/stop_jaipur/complete", "trip_multi_1", "stop_jaipur", nil, tenantID))
	assert.Equal(t, http.StatusOK, w.Code)

	// 3. Invariant: Try completing overall trip while Stop 3 is incomplete -> MUST FAIL
	w = httptest.NewRecorder()
	h.Deliver(w, stopRequest("POST", "/trips/trip_multi_1/deliver", "trip_multi_1", "", nil, tenantID))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 4. Reach and complete Stop 3
	w = httptest.NewRecorder()
	h.ReachStop(w, stopRequest("POST", "/trips/trip_multi_1/stops/stop_udaipur/reach", "trip_multi_1", "stop_udaipur", nil, tenantID))
	assert.Equal(t, http.StatusOK, w.Code)

	stop3Body, _ := json.Marshal(map[string]string{
		"pod_url":       "https://s3.example.com/pod_stop_3.jpg",
		"signature_url": "https://s3.example.com/sig_stop_3.png",
		"notes":         "Final consignment delivered",
	})
	w = httptest.NewRecorder()
	h.SubmitStopPOD(w, stopRequest("POST", "/trips/trip_multi_1/stops/stop_udaipur/pod", "trip_multi_1", "stop_udaipur", stop3Body, tenantID))
	assert.Equal(t, http.StatusOK, w.Code)

	w = httptest.NewRecorder()
	h.CompleteStop(w, stopRequest("POST", "/trips/trip_multi_1/stops/stop_udaipur/complete", "trip_multi_1", "stop_udaipur", nil, tenantID))
	assert.Equal(t, http.StatusOK, w.Code)

	// 5. Deliver and Complete Trip
	w = httptest.NewRecorder()
	h.Deliver(w, stopRequest("POST", "/trips/trip_multi_1/deliver", "trip_multi_1", "", nil, tenantID))
	assert.Equal(t, http.StatusSeeOther, w.Code)

	w = httptest.NewRecorder()
	h.CompleteTrip(w, stopRequest("POST", "/trips/trip_multi_1/complete", "trip_multi_1", "", nil, tenantID))
	assert.Equal(t, http.StatusSeeOther, w.Code)

	// 6. Verify Trip & Stops in DB
	finalTrip, err := repo.Find(context.Background(), tripID, tenantID)
	require.NoError(t, err)
	assert.Equal(t, tripagg.TripCompleted, finalTrip.Status)
	assert.Equal(t, 3, len(finalTrip.Stops))
	for _, s := range finalTrip.Stops {
		assert.Equal(t, tripagg.StopStatusCompleted, s.Status)
	}

	// 7. Verify Auto-generated Invoice for Booking
	var invoiceCount int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM invoices WHERE booking_id = 'bk_multi'`).Scan(&invoiceCount))
	assert.Equal(t, 1, invoiceCount)

	// 8. 5x Replay Protection: Replaying CompleteTrip does not duplicate invoices or corrupt state
	for i := 0; i < 5; i++ {
		w = httptest.NewRecorder()
		h.CompleteTrip(w, stopRequest("POST", "/trips/trip_multi_1/complete", "trip_multi_1", "", nil, tenantID))
		assert.Equal(t, http.StatusSeeOther, w.Code)
	}
	var invoiceCountAfterReplay int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM invoices WHERE booking_id = 'bk_multi'`).Scan(&invoiceCountAfterReplay))
	assert.Equal(t, 1, invoiceCountAfterReplay)

	// 9. Multi-Tenant Isolation: Tenant-2 cannot mutate Tenant-1 trip stops
	w = httptest.NewRecorder()
	h.ReachStop(w, stopRequest("POST", "/trips/trip_multi_1/stops/stop_jaipur/reach", "trip_multi_1", "stop_jaipur", nil, shared.TenantID("tenant-2")))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}
