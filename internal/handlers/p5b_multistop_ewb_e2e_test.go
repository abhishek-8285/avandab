package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/events"
	"transport-app/internal/ewaybill"
	geoApp "transport-app/internal/geofence/application"
	intEWB "transport-app/internal/integration/ewaybill"
	settleApp "transport-app/internal/settlement/application"
	settlesql "transport-app/internal/settlement/infrastructure/persistence/sql"
	"transport-app/internal/shared"
	tripagg "transport-app/internal/trip/domain/aggregate"
	triprepo "transport-app/internal/trip/infrastructure/persistence/sql"
)

func p5bStopRequest(method, path, tripID, stopID string, body []byte, tenant shared.TenantID) *http.Request {
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

func ptrFloat(f float64) *float64 {
	return &f
}

func ptrString(s string) *string {
	return &s
}

func TestP5B_MultiStop_EWB_Invoice_Settlement_E2EAnd5xReplay(t *testing.T) {
	db := handlerTestDB(t)
	tenantID := shared.TenantID("1")
	h := &TripHandlers{App: &App{DB: db}}

	bus := events.NewInMemoryBus()
	evaluator := geoApp.NewRealtimeEvaluator(db, bus, nil, nil)
	ewbService := ewaybill.NewEWayBillService(db, bus, intEWB.NewClient(intEWB.Config{Enabled: true, UseMock: true}), nil, ewaybill.Config{MinInvoiceValue: 10000})
	settleRepo := settlesql.NewSQLSettlementRepository(db)
	settleService := settleApp.NewSettlementAppService(settleRepo, "whsec_test", 100.0)

	ctx := context.Background()

	// 1. Seed Core Data (Customer, Route, Booking, Driver, Vehicle)
	_, err := db.Exec(`INSERT INTO customers (id, name, company, phone, gst, tenant_id) VALUES ('cust_p5b', 'Reliance Retail', 'Reliance Retail Ltd', '9876543210', '07AAAAA0000A1Z5', '1')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare, tenant_id) VALUES ('route_p5b', 'Delhi', 'Udaipur', 650, 12, 75000, '1')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO drivers (id, driver_id, first_name, last_name, phone, license_number, license_expiry, status, tenant_id) VALUES ('drv_p5b', 'DRV-P5B', 'Ramesh', 'Kumar', '9123456789', 'DL-P5B', date('now','+1 year'), 'available', '1')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, status, insurance_expiry, fitness_expiry, permit_expiry, tenant_id) VALUES ('veh_p5b', 'DL-01-AB-1234', 'DL-01-AB-1234', 'truck', 25, 'available', date('now','+1 year'), date('now','+1 year'), date('now','+1 year'), '1')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO bookings (id, booking_number, customer_id, pickup_date, route_id, vehicle_type, price, status, tenant_id) VALUES ('bk_p5b', 'BK-P5B-01', 'cust_p5b', '2026-08-30 09:00:00', 'route_p5b', 'truck', 75000, 'confirmed', '1')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO company_settings (id, company_name, currency, timezone, gst_enabled, gst_rate) VALUES (1, 'Avandab', 'INR', 'Asia/Kolkata', 1, 18) ON CONFLICT(id) DO UPDATE SET gst_enabled = 1, gst_rate = 18`)
	require.NoError(t, err)

	tripID := tripagg.TripID("trip_p5b_e2e")
	bookingID := "bk_p5b"
	now := time.Now().UTC()

	// 2. Build Multi-Stop Trip Aggregate
	trip := tripagg.NewTripAggregate(
		tripID,
		tenantID,
		"TRIP-P5B-E2E",
		&bookingID,
		"route_p5b",
		now,
		"Multi-stop freight delivery Delhi -> Jaipur -> Udaipur",
		now,
	)
	require.NoError(t, trip.Schedule(now))
	require.NoError(t, trip.AssignDriver("drv_p5b", now))
	require.NoError(t, trip.AssignVehicle("veh_p5b", now))
	require.NoError(t, trip.Start(now))

	stop1 := tripagg.TripStop{
		ID:              "stop_1_delhi",
		TenantID:        tenantID,
		TripID:          tripID,
		StopSequence:    1,
		StopType:        tripagg.StopTypePickup,
		LocationName:    "Delhi Primary Hub",
		Address:         "Connaught Place, New Delhi",
		Latitude:        ptrFloat(28.6139),
		Longitude:       ptrFloat(77.2090),
		GeofenceRadiusM: 100,
		Status:          tripagg.StopStatusPending,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	stop2 := tripagg.TripStop{
		ID:              "stop_2_jaipur",
		TenantID:        tenantID,
		TripID:          tripID,
		StopSequence:    2,
		StopType:        tripagg.StopTypeDrop,
		LocationName:    "Jaipur Intermediate Depot",
		Address:         "MI Road, Jaipur",
		Latitude:        ptrFloat(26.9124),
		Longitude:       ptrFloat(75.7873),
		GeofenceRadiusM: 100,
		Status:          tripagg.StopStatusPending,
		PODRequired:     true,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	stop3 := tripagg.TripStop{
		ID:              "stop_3_udaipur",
		TenantID:        tenantID,
		TripID:          tripID,
		StopSequence:    3,
		StopType:        tripagg.StopTypeDrop,
		LocationName:    "Udaipur Final Depot",
		Address:         "City Palace Rd, Udaipur",
		Latitude:        ptrFloat(24.5854),
		Longitude:       ptrFloat(73.7125),
		GeofenceRadiusM: 100,
		Status:          tripagg.StopStatusPending,
		PODRequired:     true,
		OTPRequired:     true,
		OTPCode:         "7788",
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	trip.AddStop(stop1)
	trip.AddStop(stop2)
	trip.AddStop(stop3)

	repo := triprepo.NewTripRepository(db)
	require.NoError(t, repo.Save(ctx, trip))

	// 3. Generate E-Way Bill Part-A and Attach Part-B
	ewbRec, err := ewbService.GeneratePartA(ctx, ewaybill.GeneratePartARequest{
		TripID:     string(tripID),
		GoodsValue: 75000,
		Distance:   650,
		GenMode:    "AUTO",
		Force:      true,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, ewbRec.EwbNumber)

	_, err = ewbService.AttachPartB(ctx, ewbRec.EwbNumber, "DL-01-AB-1234", "")
	require.NoError(t, err)

	// ==========================================
	// EXECUTION LOOP (Delhi -> Jaipur -> Udaipur)
	// ==========================================

	// --- STOP 1: Delhi Pickup ---
	// Geofence Trigger Arrival
	_, _ = evaluator.EvaluateFix(ctx, geoApp.TelemetryFix{
		TenantID: string(tenantID), VehicleID: "veh_p5b", TripID: ptrString(string(tripID)),
		Latitude: 28.6139, Longitude: 77.2090, Timestamp: now,
	})
	_, _ = evaluator.EvaluateFix(ctx, geoApp.TelemetryFix{
		TenantID: string(tenantID), VehicleID: "veh_p5b", TripID: ptrString(string(tripID)),
		Latitude: 28.6139, Longitude: 77.2090, Timestamp: now.Add(65 * time.Second),
	})

	// HTTP Complete Stop 1
	w := httptest.NewRecorder()
	h.CompleteStop(w, p5bStopRequest("POST", fmt.Sprintf("/trips/%s/stops/stop_1_delhi/complete", tripID), string(tripID), "stop_1_delhi", nil, tenantID))
	assert.Equal(t, http.StatusOK, w.Code)

	// Transition trip to in_transit
	w = httptest.NewRecorder()
	h.ReachPickup(w, p5bStopRequest("POST", fmt.Sprintf("/trips/%s/reach-pickup", tripID), string(tripID), "", nil, tenantID))
	w = httptest.NewRecorder()
	h.StartTransit(w, p5bStopRequest("POST", fmt.Sprintf("/trips/%s/in-transit", tripID), string(tripID), "", nil, tenantID))

	// --- STOP 2: Jaipur Intermediate Drop ---
	// Geofence Trigger Arrival
	tJaipur := now.Add(5 * time.Hour)
	_, _ = evaluator.EvaluateFix(ctx, geoApp.TelemetryFix{
		TenantID: string(tenantID), VehicleID: "veh_p5b", TripID: ptrString(string(tripID)),
		Latitude: 26.9124, Longitude: 75.7873, Timestamp: tJaipur,
	})
	_, _ = evaluator.EvaluateFix(ctx, geoApp.TelemetryFix{
		TenantID: string(tenantID), VehicleID: "veh_p5b", TripID: ptrString(string(tripID)),
		Latitude: 26.9124, Longitude: 75.7873, Timestamp: tJaipur.Add(65 * time.Second),
	})

	// HTTP Submit Stop 2 POD
	podBody2, _ := json.Marshal(map[string]string{
		"pod_url":       "https://s3.example.com/pod_jaipur.jpg",
		"signature_url": "https://s3.example.com/sig_jaipur.png",
		"notes":         "Delivered 10 crates in good condition",
	})
	w = httptest.NewRecorder()
	h.SubmitStopPOD(w, p5bStopRequest("POST", fmt.Sprintf("/trips/%s/stops/stop_2_jaipur/pod", tripID), string(tripID), "stop_2_jaipur", podBody2, tenantID))
	assert.Equal(t, http.StatusOK, w.Code)

	// HTTP Complete Stop 2
	w = httptest.NewRecorder()
	h.CompleteStop(w, p5bStopRequest("POST", fmt.Sprintf("/trips/%s/stops/stop_2_jaipur/complete", tripID), string(tripID), "stop_2_jaipur", nil, tenantID))
	assert.Equal(t, http.StatusOK, w.Code)

	// --- STOP 3: Udaipur Final Drop ---
	// Geofence Trigger Arrival
	tUdaipur := now.Add(12 * time.Hour)
	_, _ = evaluator.EvaluateFix(ctx, geoApp.TelemetryFix{
		TenantID: string(tenantID), VehicleID: "veh_p5b", TripID: ptrString(string(tripID)),
		Latitude: 24.5854, Longitude: 73.7125, Timestamp: tUdaipur,
	})
	_, _ = evaluator.EvaluateFix(ctx, geoApp.TelemetryFix{
		TenantID: string(tenantID), VehicleID: "veh_p5b", TripID: ptrString(string(tripID)),
		Latitude: 24.5854, Longitude: 73.7125, Timestamp: tUdaipur.Add(65 * time.Second),
	})

	// HTTP Submit Stop 3 POD & OTP
	podBody3, _ := json.Marshal(map[string]string{
		"pod_url":       "https://s3.example.com/pod_udaipur.jpg",
		"signature_url": "https://s3.example.com/sig_udaipur.png",
		"otp":           "7788",
		"notes":         "Final consignment received with valid OTP",
	})
	w = httptest.NewRecorder()
	h.SubmitStopPOD(w, p5bStopRequest("POST", fmt.Sprintf("/trips/%s/stops/stop_3_udaipur/pod", tripID), string(tripID), "stop_3_udaipur", podBody3, tenantID))
	assert.Equal(t, http.StatusOK, w.Code)

	// HTTP Complete Stop 3
	w = httptest.NewRecorder()
	h.CompleteStop(w, p5bStopRequest("POST", fmt.Sprintf("/trips/%s/stops/stop_3_udaipur/complete", tripID), string(tripID), "stop_3_udaipur", nil, tenantID))
	assert.Equal(t, http.StatusOK, w.Code)

	// Deliver & Complete Trip
	w = httptest.NewRecorder()
	h.Deliver(w, p5bStopRequest("POST", fmt.Sprintf("/trips/%s/deliver", tripID), string(tripID), "", nil, tenantID))
	assert.Equal(t, http.StatusSeeOther, w.Code)

	// Seed closed detention to satisfy CompleteTrip auto-generation invariant
	_, err = db.Exec(`INSERT INTO geofences (id, tenant_id, name, kind, shape, center_lat, center_lng, radius_m, priority, is_active) VALUES ('g_p5b', '1', 'Udaipur Depot', 'depot', 'circle', 24.5, 73.7, 500, 10, 1)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO trip_detentions (id, tenant_id, trip_id, vehicle_id, geofence_id, zone_kind, entered_at, exited_at, dwell_seconds, free_seconds, billable_seconds, rate_per_hour, amount, status) VALUES ('det_p5b_1', '1', 'trip_p5b_e2e', NULL, 'g_p5b', 'drop', '2026-08-30 21:00:00', '2026-08-30 23:00:00', 7200, 1800, 5400, 100, 150.0, 'closed')`)
	require.NoError(t, err)

	w = httptest.NewRecorder()
	h.CompleteTrip(w, p5bStopRequest("POST", fmt.Sprintf("/trips/%s/complete", tripID), string(tripID), "", nil, tenantID))
	assert.Equal(t, http.StatusSeeOther, w.Code)

	// Generate Driver Settlement
	settle, err := settleService.CalculateAndCreateSettlement(ctx, string(tenantID), settleApp.CalculateSettlementRequest{
		TripID:            string(tripID),
		DriverID:          "drv_p5b",
		GrossFare:         75000.0,
		TollAdjustment:    500.0,
		AdvanceDeductions: 2000.0,
		CommissionRate:    0.10,
		TDSRate:           0.01,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, settle.ID)

	// ==========================================
	// 5x REPLAY OF ENTIRE SEQUENCE
	// ==========================================
	for replay := 1; replay <= 5; replay++ {
		// Replay EWB Part-A & Part-B
		_, errA := ewbService.GeneratePartA(ctx, ewaybill.GeneratePartARequest{TripID: string(tripID), GoodsValue: 75000, Distance: 650, Force: true})
		assert.NoError(t, errA)

		// Replay Stop completions
		wRep := httptest.NewRecorder()
		h.CompleteStop(wRep, p5bStopRequest("POST", fmt.Sprintf("/trips/%s/stops/stop_3_udaipur/complete", tripID), string(tripID), "stop_3_udaipur", nil, tenantID))

		// Replay Trip completion
		wTrip := httptest.NewRecorder()
		h.CompleteTrip(wTrip, p5bStopRequest("POST", fmt.Sprintf("/trips/%s/complete", tripID), string(tripID), "", nil, tenantID))

		// Replay Settlement
		_, _ = settleService.CalculateAndCreateSettlement(ctx, string(tenantID), settleApp.CalculateSettlementRequest{
			TripID:            string(tripID),
			DriverID:          "drv_p5b",
			GrossFare:         75000.0,
			TollAdjustment:    500.0,
			AdvanceDeductions: 2000.0,
			CommissionRate:    0.10,
			TDSRate:           0.01,
		})
	}

	// ==========================================
	// STRICT ACCEPTANCE ASSERTIONS
	// ==========================================

	// 1. Trip completion = 1
	var tripStatus string
	require.NoError(t, db.QueryRow(`SELECT status FROM trips WHERE id = ?`, tripID).Scan(&tripStatus))
	assert.Equal(t, "completed", tripStatus)

	// 2. Invoice = 1 logical document
	var invCount int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM invoices WHERE booking_id = 'bk_p5b'`).Scan(&invCount))
	assert.Equal(t, 1, invCount, "Exactly one commercial invoice must exist for the multi-stop booking")

	// 3. EWB = 1 logical document
	var ewbCount int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM eway_bills WHERE trip_id = ?`, tripID).Scan(&ewbCount))
	assert.Equal(t, 1, ewbCount, "Exactly one logical EWB must exist for the multi-stop trip")

	// 4. Stop transitions = 3 completed
	var completedStops int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM trip_stops WHERE trip_id = ? AND status = 'completed'`, tripID).Scan(&completedStops))
	assert.Equal(t, 3, completedStops, "All 3 stops must be in completed status")

	// 5. PODs = exactly required set
	var podVerifiedCount int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM trip_stops WHERE trip_id = ? AND pod_verified_at IS NOT NULL`, tripID).Scan(&podVerifiedCount))
	assert.Equal(t, 2, podVerifiedCount, "Exactly 2 drops (Stop 2 and Stop 3) must have verified PODs")

	// 6. Settlement = 1
	var settleCount int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM driver_settlements WHERE trip_id = ?`, tripID).Scan(&settleCount))
	assert.Equal(t, 1, settleCount, "Exactly one driver settlement record must exist")

	// 7. Ledger entries = no duplicate replay entries
	var ledgerCount int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM driver_ledger_entries WHERE trip_id = ?`, tripID).Scan(&ledgerCount))
	assert.Equal(t, 5, ledgerCount, "Driver ledger must contain exactly one set of earning, commission, toll, advance, and TDS entries without duplicate replay rows")
}
