package test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/events"
	"transport-app/internal/ewaybill"
	"transport-app/internal/fastag"
	"transport-app/internal/handlers"
	intEWB "transport-app/internal/integration/ewaybill"
	intFastag "transport-app/internal/integration/fastag"
)

func TestEWayBill_Lifecycle_And_Gating(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()

	// Seed customer, route, vehicle, booking, trip
	_, err := db.Exec(`INSERT INTO customers (id, name, email, phone, gst) VALUES ('cust-ewb', 'Logistics Hub', 'hub@example.com', '9876543210', '07AAACP0000M1Z9')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO vehicles (id, vehicle_number, registration_number, vehicle_type, capacity, insurance_expiry, fitness_expiry, permit_expiry, status) VALUES ('veh-ewb-1', 'V-01', 'MH12AB1234', 'truck', 10, '2028-12-31', '2028-12-31', '2028-12-31', 'available')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare) VALUES ('route-ewb-1', 'Mumbai', 'Pune', 150.0, 3.0, 60000.0)`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO bookings (id, tenant_id, booking_number, customer_id, route_id, pickup_date, vehicle_type, status, price) VALUES ('book-ewb-1', '1', 'BKG-EWB-1', 'cust-ewb', 'route-ewb-1', '2026-08-20', 'TRUCK', 'confirmed', 75000.0)`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO trips (id, tenant_id, trip_number, booking_id, route_id, vehicle_id, status, departure_time) VALUES ('trip-ewb-1', '1', 'TRP-EWB-1', 'book-ewb-1', 'route-ewb-1', 'veh-ewb-1', 'in_transit', datetime('now', '-2 hours'))`)
	require.NoError(t, err)

	bus := events.NewInMemoryBus()
	cfg := ewaybill.Config{
		Enabled:              true,
		ExtensionKM:          10.0,
		ExtensionLeadSeconds: 14400,
		MinInvoiceValue:      50000.0,
	}
	client := intEWB.NewClient(intEWB.Config{Enabled: true, UseMock: true})
	svc := ewaybill.NewEWayBillService(db, bus, client, nil, cfg)

	// 1. Generate Part-A manually
	rec, err := svc.GeneratePartA(ctx, ewaybill.GeneratePartARequest{
		TripID:  "trip-ewb-1",
		GenMode: "MANUAL",
	})
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.NotEmpty(t, rec.EwbNumber)
	assert.Equal(t, "MANUAL", rec.GenMode)
	assert.Equal(t, 75000.0, rec.GoodsValue)

	// Verify DB state & trip ref
	var tripRef sql.NullString
	err = db.QueryRow(`SELECT eway_bill_ref FROM trips WHERE id = 'trip-ewb-1'`).Scan(&tripRef)
	require.NoError(t, err)
	assert.Equal(t, rec.EwbNumber, tripRef.String)

	// Verify eway_bill_events
	var eventType string
	err = db.QueryRow(`SELECT event_type FROM eway_bill_events WHERE ewb_number = ? AND event_type = 'PART_A_GENERATED'`, rec.EwbNumber).Scan(&eventType)
	require.NoError(t, err)
	assert.Equal(t, "PART_A_GENERATED", eventType)

	// 2. Attach Part-B
	attached, err := svc.AttachPartB(ctx, rec.EwbNumber, "MH12AB1234", "TRANS123")
	require.NoError(t, err)
	assert.Equal(t, "active", attached.Status)
	assert.NotNil(t, attached.VehicleNumber)
	assert.Equal(t, "MH12AB1234", *attached.VehicleNumber)

	// Verify event
	err = db.QueryRow(`SELECT event_type FROM eway_bill_events WHERE ewb_number = ? AND event_type = 'PART_B_ADDED'`, rec.EwbNumber).Scan(&eventType)
	require.NoError(t, err)
	assert.Equal(t, "PART_B_ADDED", eventType)

	// 3. Extend without geofence evidence -> Must be denied
	// Ensure no telemetry exists yet
	_, err = svc.Extend(ctx, rec.EwbNumber, ewaybill.ExtendRequest{
		EwbNumber: rec.EwbNumber,
		Reason:    "traffic_delay",
	})
	assert.Error(t, err)
	// With destination geofence present, if vehicle is too far, it is denied:
	_, err = db.Exec(`INSERT INTO geofences (id, tenant_id, name, kind, shape, center_lat, center_lng, radius_m, route_name, is_active) VALUES ('geo-dest', '1', 'Pune Dest', 'drop', 'circle', 18.5204, 73.8567, 500, 'route-ewb-1', 1)`)
	require.NoError(t, err)
	// Add far position (Mumbai)
	_, err = db.Exec(`INSERT INTO vehicle_latest_position (vehicle_id, tenant_id, device_time, latitude, longitude) VALUES ('veh-ewb-1', '1', datetime('now'), 19.0760, 72.8777)`)
	require.NoError(t, err)

	_, err = svc.Extend(ctx, rec.EwbNumber, ewaybill.ExtendRequest{
		EwbNumber: rec.EwbNumber,
		Reason:    "traffic_delay",
	})
	assert.ErrorIs(t, err, ewaybill.ErrNoGeofenceEvidence)

	// Verify EXTENSION_DENIED event
	err = db.QueryRow(`SELECT event_type FROM eway_bill_events WHERE ewb_number = ? AND event_type = 'EXTENSION_DENIED'`, rec.EwbNumber).Scan(&eventType)
	require.NoError(t, err)

	// 4. Extend with geofence evidence (vehicle near destination in Pune)
	_, err = db.Exec(`UPDATE vehicle_latest_position SET latitude = 18.5205, longitude = 73.8568, device_time = datetime('now', '+1 minute') WHERE vehicle_id = 'veh-ewb-1'`)
	require.NoError(t, err)

	extended, err := svc.Extend(ctx, rec.EwbNumber, ewaybill.ExtendRequest{
		EwbNumber: rec.EwbNumber,
		Reason:    "unloading_queue",
	})
	require.NoError(t, err)
	assert.Equal(t, 1, extended.ExtensionCount)

	// 5. Extend limit exceeded (2nd extension must fail)
	_, err = svc.Extend(ctx, rec.EwbNumber, ewaybill.ExtendRequest{
		EwbNumber: rec.EwbNumber,
		Reason:    "second_extension",
	})
	assert.ErrorIs(t, err, ewaybill.ErrExtensionLimitExceeded)

	// 6. Cancel EWB
	cancelled, err := svc.Cancel(ctx, rec.EwbNumber, "customer_cancelled")
	require.NoError(t, err)
	assert.Equal(t, "cancelled", cancelled.Status)
	assert.Equal(t, "customer_cancelled", cancelled.CancelReason)
}

func TestEWayBill_AutoGenerate_Subscriber(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()

	// Seed customer, route, bookings
	_, err := db.Exec(`INSERT INTO customers (id, name, email, phone, gst) VALUES ('cust-auto', 'Auto Logistics', 'auto@example.com', '9876543210', '07AAACP0000M1Z9')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare) VALUES ('route-auto', 'Delhi', 'Jaipur', 280.0, 5.0, 55000.0)`)
	require.NoError(t, err)

	// High value booking (> 50k)
	_, err = db.Exec(`INSERT INTO bookings (id, tenant_id, booking_number, customer_id, route_id, pickup_date, vehicle_type, status, price) VALUES ('book-high', '1', 'BKG-HIGH', 'cust-auto', 'route-auto', '2026-08-20', 'TRUCK', 'confirmed', 80000.0)`)
	require.NoError(t, err)

	// Low value booking (<= 50k)
	_, err = db.Exec(`INSERT INTO bookings (id, tenant_id, booking_number, customer_id, route_id, pickup_date, vehicle_type, status, price) VALUES ('book-low', '1', 'BKG-LOW', 'cust-auto', 'route-auto', '2026-08-20', 'TRUCK', 'confirmed', 40000.0)`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO trips (id, tenant_id, trip_number, booking_id, route_id, status, departure_time) VALUES ('trip-high', '1', 'TRP-HIGH', 'book-high', 'route-auto', 'scheduled', '2026-08-20 10:00:00')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO trips (id, tenant_id, trip_number, booking_id, route_id, status, departure_time) VALUES ('trip-low', '1', 'TRP-LOW', 'book-low', 'route-auto', 'scheduled', '2026-08-20 10:00:00')`)
	require.NoError(t, err)

	bus := events.NewInMemoryBus()
	cfg := ewaybill.Config{
		Enabled:         true,
		MinInvoiceValue: 50000.0,
	}
	client := intEWB.NewClient(intEWB.Config{Enabled: true, UseMock: true})
	svc := ewaybill.NewEWayBillService(db, bus, client, nil, cfg)
	svc.SubscribeTripEvents(bus)

	// Publish TripConfirmedEvent for high-value trip
	bus.Publish(ctx, events.Event{
		Type: "TripConfirmedEvent",
		Payload: map[string]interface{}{
			"TripID": "trip-high",
		},
	})

	// Verify EWB was auto-generated
	var ewbCount int
	err = db.QueryRow(`SELECT COUNT(*) FROM eway_bills WHERE trip_id = 'trip-high' AND gen_mode = 'AUTO'`).Scan(&ewbCount)
	require.NoError(t, err)
	assert.Equal(t, 1, ewbCount)

	// Publish TripConfirmedEvent for low-value trip
	bus.Publish(ctx, events.Event{
		Type: "TripConfirmedEvent",
		Payload: map[string]interface{}{
			"TripID": "trip-low",
		},
	})

	// Verify low-value trip did NOT generate EWB
	err = db.QueryRow(`SELECT COUNT(*) FROM eway_bills WHERE trip_id = 'trip-low'`).Scan(&ewbCount)
	require.NoError(t, err)
	assert.Equal(t, 0, ewbCount)
}

func TestFASTag_DB_Balance_And_Deduct(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()

	// Seed fastag_tags row with custom balance
	_, err := db.Exec(`
		INSERT INTO fastag_tags (id, tenant_id, tag_id, vehicle_number, issuer, tag_class, balance, status, last_sync)
		VALUES ('ft-1', '1', 'TAG-MH12-999', 'MH12CD5678', 'ICICI', 'VC4', 3500.00, 'ACTIVE', datetime('now'))
	`)
	require.NoError(t, err)

	client := intFastag.NewClient(intFastag.Config{Enabled: true, UseMock: true}, db)
	svc := fastag.NewFASTagService(db, client, fastag.Config{AutoKharcha: true})

	// 1. Get balance should read DB balance (3500.00), not hardcoded 2475.50
	bal, err := svc.GetBalance(ctx, "MH12CD5678")
	require.NoError(t, err)
	assert.Equal(t, 3500.00, bal.Balance)
	assert.Equal(t, "TAG-MH12-999", bal.TagID)

	// 2. Deduct toll 250.00
	txn, err := svc.DeductToll(ctx, intFastag.DeductTollRequest{
		VehicleNumber: "MH12CD5678",
		TagID:         "TAG-MH12-999",
		PlazaID:       "PLZ-KHALAPUR",
		PlazaName:     "Khalapur Toll Plaza",
		Amount:        250.00,
	})
	require.NoError(t, err)
	assert.Equal(t, 250.00, txn.Amount)

	// 3. Verify updated balance in DB (3500 - 250 = 3250)
	balAfter, err := svc.GetBalance(ctx, "MH12CD5678")
	require.NoError(t, err)
	assert.Equal(t, 3250.00, balAfter.Balance)
}

func TestFASTag_Reconciliation_Greedy_And_AutoKharcha(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()

	// Seed driver, vehicle, trip
	_, err := db.Exec(`INSERT INTO drivers (id, driver_id, first_name, last_name, phone, license_number, license_expiry, status) VALUES ('drv-recon', 'DRV-RECON-1', 'Ramesh', 'Kumar', '9876543210', 'DL-MH-12345', '2028-12-31', 'available')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO vehicles (id, vehicle_number, registration_number, vehicle_type, capacity, insurance_expiry, fitness_expiry, permit_expiry, status) VALUES ('veh-recon', 'V-RECON', 'MH12EF9012', 'truck', 15, '2028-12-31', '2028-12-31', '2028-12-31', 'available')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare) VALUES ('route-recon', 'Mumbai', 'Nashik', 180.0, 4.0, 20000.0)`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO bookings (id, tenant_id, booking_number, customer_id, route_id, pickup_date, vehicle_type, status, price) VALUES ('book-recon', '1', 'BKG-RECON', 'cust-1', 'route-recon', '2026-08-20', 'TRUCK', 'confirmed', 25000.0)`)
	require.NoError(t, err)

	// Trip window: departure 10:00, arrival 14:00 today
	now := time.Now().UTC()
	depTime := now.Add(-3 * time.Hour)
	arrTime := now.Add(1 * time.Hour)

	_, err = db.Exec(`
		INSERT INTO trips (id, tenant_id, trip_number, booking_id, route_id, vehicle_id, driver_id, status, departure_time, arrival_time)
		VALUES ('trip-recon-1', '1', 'TRP-RECON-1', 'book-recon', 'route-recon', 'veh-recon', 'drv-recon', 'in_transit', ?, ?)
	`, depTime, arrTime)
	require.NoError(t, err)

	// Seed unreconciled FASTag transaction during trip window (depTime + 1h)
	txnTime := depTime.Add(1 * time.Hour)
	_, err = db.Exec(`
		INSERT INTO fastag_transactions (id, tenant_id, tag_id, vehicle_number, plaza_id, plaza_name, amount, txn_timestamp, status, source, reconciled)
		VALUES ('tx-recon-1', '1', 'TAG-RECON-1', 'MH12EF9012', 'PLZ-KASARA', 'Kasara Ghat Toll', 135.00, ?, 'SUCCESS', 'PROVIDER', 0)
	`, txnTime)
	require.NoError(t, err)

	client := intFastag.NewClient(intFastag.Config{Enabled: true, UseMock: true}, db)
	svc := fastag.NewFASTagService(db, client, fastag.Config{AutoKharcha: true})

	// Run Reconcile
	res, err := svc.Reconcile(ctx, "MH12EF9012", "", "")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, res.Matched, 1)
	assert.GreaterOrEqual(t, res.KharchaCreated, 1)

	// Verify fastag_transactions row is marked reconciled and linked to trip
	var reconciled int
	var matchedTripID, kharchaID sql.NullString
	err = db.QueryRow(`SELECT reconciled, trip_id, kharcha_id FROM fastag_transactions WHERE id = 'tx-recon-1'`).Scan(&reconciled, &matchedTripID, &kharchaID)
	require.NoError(t, err)
	assert.Equal(t, 1, reconciled)
	assert.Equal(t, "trip-recon-1", matchedTripID.String)
	assert.True(t, kharchaID.Valid)

	// Verify driver_expenses row created
	var expAmount float64
	var expCategory, expType, expStatus string
	err = db.QueryRow(`SELECT amount, category, expense_type, status FROM driver_expenses WHERE id = ?`, kharchaID.String).Scan(&expAmount, &expCategory, &expType, &expStatus)
	require.NoError(t, err)
	assert.Equal(t, 135.00, expAmount)
	assert.Equal(t, "toll", expCategory)
	assert.Equal(t, "toll", expType)
	assert.Equal(t, "approved", expStatus)
}

func TestEWayBill_And_FASTag_HTTP_Handlers(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// Seed minimal data
	_, err := db.Exec(`INSERT INTO customers (id, name, email, phone, gst) VALUES ('cust-http', 'HTTP Logistics', 'http@example.com', '9876543210', '07AAACP0000M1Z9')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare) VALUES ('route-http', 'Mumbai', 'Delhi', 1400.0, 24.0, 80000.0)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO bookings (id, tenant_id, booking_number, customer_id, route_id, pickup_date, vehicle_type, status, price) VALUES ('book-http', '1', 'BKG-HTTP', 'cust-http', 'route-http', '2026-08-20', 'TRUCK', 'confirmed', 90000.0)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO trips (id, tenant_id, trip_number, booking_id, route_id, status, departure_time) VALUES ('trip-http', '1', 'TRP-HTTP', 'book-http', 'route-http', 'scheduled', '2026-08-20 10:00:00')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO fastag_tags (id, tenant_id, tag_id, vehicle_number, issuer, tag_class, balance, status, last_sync) VALUES ('tag-http', '1', 'TAG-HTTP-1', 'MH12HTTP99', 'HDFC', 'VC4', 5000.00, 'ACTIVE', datetime('now'))`)
	require.NoError(t, err)

	bus := events.NewInMemoryBus()
	ewbClient := intEWB.NewClient(intEWB.Config{Enabled: true, UseMock: true})
	ewbSvc := ewaybill.NewEWayBillService(db, bus, ewbClient, nil, ewaybill.Config{Enabled: true})
	fastagClient := intFastag.NewClient(intFastag.Config{Enabled: true, UseMock: true}, db)
	fastagSvc := fastag.NewFASTagService(db, fastagClient, fastag.Config{AutoKharcha: true})

	ewbHandlers := handlers.NewEWayBillHandlers(nil, ewbSvc, &stubAuthSvc{})
	fastagHandlers := handlers.NewFASTagHandlers(nil, fastagSvc, &stubAuthSvc{})

	r := chi.NewRouter()
	r.Use(authInjectMiddleware)
	ewbHandlers.Mount(r)
	fastagHandlers.Mount(r)

	// 1. POST /trips/trip-http/ewaybill -> generate Part-A
	req := httptest.NewRequest(http.MethodPost, "/trips/trip-http/ewaybill", nil)
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code, "body: %s", w.Body.String())

	var ewbRes map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &ewbRes)
	require.NoError(t, err)
	ewbNum := ewbRes["ewb_number"].(string)
	assert.NotEmpty(t, ewbNum)

	// 2. GET /trips/trip-http/ewaybill -> read back
	req = httptest.NewRequest(http.MethodGet, "/trips/trip-http/ewaybill", nil)
	req.Header.Set("Accept", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// 3. POST /ewaybill/{ewb}/part-b -> attach vehicle
	form := url.Values{}
	form.Set("vehicle_number", "MH12HTTP99")
	form.Set("transporter_id", "TR-001")
	req = httptest.NewRequest(http.MethodPost, "/ewaybill/"+ewbNum+"/part-b", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// 4. GET /fastag/balance?vehicle_number=MH12HTTP99
	req = httptest.NewRequest(http.MethodGet, "/fastag/balance?vehicle_number=MH12HTTP99", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var balRes map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &balRes)
	require.NoError(t, err)
	assert.Equal(t, 5000.0, balRes["balance"])

	// 5. POST /fastag/deduct
	deductBody := `{"vehicle_number":"MH12HTTP99","tag_id":"TAG-HTTP-1","plaza_id":"PLZ-1","plaza_name":"Expressway Plaza","amount":300.0}`
	req = httptest.NewRequest(http.MethodPost, "/fastag/deduct", strings.NewReader(deductBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	// 6. POST /fastag/reconcile
	req = httptest.NewRequest(http.MethodPost, "/fastag/reconcile", strings.NewReader(`{"vehicle_number":"MH12HTTP99"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
}
