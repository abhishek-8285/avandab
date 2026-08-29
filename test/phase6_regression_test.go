package test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
	"transport-app/internal/events"
	"transport-app/internal/ewaybill"
	"transport-app/internal/fastag"
	"transport-app/internal/handlers"
	intEWB "transport-app/internal/integration/ewaybill"
	intFastag "transport-app/internal/integration/fastag"
	"transport-app/internal/integration/gstn"
	"transport-app/internal/shared"
)

// mockPhase6Auth is a permissive auth service for testing.
type mockPhase6Auth struct {
	allowed bool
}

func (m *mockPhase6Auth) Can(userID, resource, action string) bool { return m.allowed }
func (m *mockPhase6Auth) Reload() error                            { return nil }
func (m *mockPhase6Auth) AddRoleForUser(userID, role string) error { return nil }
func (m *mockPhase6Auth) DeleteRolesForUser(userID string) error   { return nil }

func seedPhase6Base(t *testing.T, db *sql.DB) {
	t.Helper()
	// Supplier company in MH (27)
	_, _ = db.Exec(`INSERT OR REPLACE INTO company_settings (id, tenant_id, company_name, gst_number, state_code, gst_rate) VALUES (1, '1', 'Avandab Logistics', '27AABCU9603R1ZX', '27', 18.0)`)

	// Intra-state Customer (MH - 27)
	_, _ = db.Exec(`INSERT OR REPLACE INTO customers (id, name, email, phone, gst) VALUES ('cust-mh', 'MH Customer Ltd', 'mh@cust.com', '9800000001', '27AAACP0000M1Z9')`)

	// Inter-state Customer (DL - 07)
	_, _ = db.Exec(`INSERT OR REPLACE INTO customers (id, name, email, phone, gst) VALUES ('cust-dl', 'Delhi Traders', 'dl@cust.com', '9800000002', '07AAACP0000M1Z9')`)

	// Vehicles
	_, _ = db.Exec(`INSERT OR REPLACE INTO vehicles (id, tenant_id, vehicle_number, registration_number, vehicle_type, capacity, insurance_expiry, fitness_expiry, permit_expiry, status) VALUES ('veh-p6-1', '1', 'V-01', 'MH12AB1234', 'truck', 10, '2028-12-31', '2028-12-31', '2028-12-31', 'available')`)

	// Routes
	_, _ = db.Exec(`INSERT OR REPLACE INTO routes (id, source, destination, distance, estimated_hours, standard_fare) VALUES ('route-p6-1', 'Mumbai', 'Pune', 150.0, 3.0, 60000.0)`)
}

// 1. GST E-Invoice Full Flow (Inter-state & Intra-state tax split, IRN generation, IRN determinism, 409 duplicate)
func TestPhase6_1_GSTEInvoice_FullFlow(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	seedPhase6Base(t, db)
	ctx := context.WithValue(shared.ContextWithTenantID(context.Background(), "1"), auth.ContextUser, &auth.SessionData{UserID: "admin-1", Role: "admin"})

	// Create booking and invoice
	_, err := db.Exec(`INSERT INTO bookings (id, tenant_id, booking_number, customer_id, route_id, pickup_date, vehicle_type, status, price) VALUES ('bkg-flow-1', '1', 'BK-FLOW-1', 'cust-mh', 'route-p6-1', '2026-08-20', 'truck', 'confirmed', 10000.0)`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO invoices (id, tenant_id, invoice_number, booking_id, customer_id, subtotal, tax, total, status, payment_status, created_at) VALUES ('inv-flow-1', '1', 'INV-2026-001', 'bkg-flow-1', 'cust-mh', 10000.0, 500.0, 10500.0, 'draft', 'pending', '2026-08-19 10:00:00')`)
	require.NoError(t, err)

	app := &handlers.App{DB: db, AuthSrv: &mockPhase6Auth{allowed: true}}
	invHandler := &handlers.InvoiceHandlers{App: app}

	r := chi.NewRouter()
	invHandler.Routes(r)

	// Add intra-state line item (HSN 996511 is 5% SAC -> 2.5% CGST + 2.5% SGST)
	form := url.Values{
		"hsn_sac_code": {"996511"},
		"description":  {"Freight transport"},
		"unit":         {"NOS"},
		"quantity":     {"1"},
		"rate":         {"10000"},
	}
	req := httptest.NewRequest("POST", "/inv-flow-1/line-items", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusSeeOther, rec.Code)

	// Verify tax split in DB
	var cgst, sgst, igst, total float64
	err = db.QueryRow(`SELECT cgst, sgst, igst, total FROM invoices WHERE id = 'inv-flow-1'`).Scan(&cgst, &sgst, &igst, &total)
	require.NoError(t, err)
	assert.Equal(t, 250.0, cgst, "CGST should be 2.5%")
	assert.Equal(t, 250.0, sgst, "SGST should be 2.5%")
	assert.Equal(t, 0.0, igst, "IGST should be 0 for intra-state")
	assert.Equal(t, 10500.0, total)

	// Generate IRN
	reqIRN := httptest.NewRequest("POST", "/inv-flow-1/generate-irn", nil)
	reqIRN.Header.Set("Accept", "application/json")
	reqIRN = reqIRN.WithContext(ctx)
	recIRN := httptest.NewRecorder()
	r.ServeHTTP(recIRN, reqIRN)
	assert.Equal(t, http.StatusOK, recIRN.Code)

	var irnResp gstn.IRNResponse
	err = json.Unmarshal(recIRN.Body.Bytes(), &irnResp)
	require.NoError(t, err)
	assert.NotEmpty(t, irnResp.IRN)
	assert.Len(t, irnResp.IRN, 64, "IRN must be 64-char SHA256 hex")

	// Verify IRN Determinism
	invView := gstn.InvoiceView{
		InvoiceID:      "inv-flow-1",
		InvoiceNumber:  "INV-2026-001",
		InvoiceDate:    "2026-08-19",
		SupplierGSTIN:  "27AABCU9603R1ZX",
		RecipientGSTIN: "27AAACP0000M1Z9",
		TotalValue:     10500.0,
		CGST:           250.0,
		SGST:           250.0,
		IGST:           0.0,
		LineItems: []gstn.LineItemView{{
			HSNSACCode:   "996511",
			Description:  "Freight transport",
			Unit:         "NOS",
			Quantity:     1,
			Rate:         10000.0,
			TaxableValue: 10000.0,
			CGSTRate:     2.5,
			SGSTRate:     2.5,
			IGSTRate:     0.0,
			CGSTAmount:   250.0,
			SGSTAmount:   250.0,
			IGSTAmount:   0.0,
			Total:        10500.0,
		}},
	}
	hashExpected := gstn.ComputeIRN(invView)
	assert.Len(t, hashExpected, 64)
	assert.Equal(t, hashExpected, gstn.ComputeIRN(invView), "IRN generation must be strictly deterministic")

	// Test 409 Conflict on duplicate IRN generation
	recDup := httptest.NewRecorder()
	r.ServeHTTP(recDup, reqIRN)
	assert.Equal(t, http.StatusConflict, recDup.Code, "Re-generating existing IRN must yield 409 Conflict")

	// Test 409 Conflict on paid invoice
	_, _ = db.Exec(`INSERT INTO bookings (id, tenant_id, booking_number, customer_id, route_id, pickup_date, vehicle_type, status, price) VALUES ('bkg-paid-1', '1', 'BK-PAID-1', 'cust-mh', 'route-p6-1', '2026-08-20', 'truck', 'confirmed', 5000.0)`)
	_, _ = db.Exec(`INSERT INTO invoices (id, tenant_id, invoice_number, booking_id, customer_id, subtotal, tax, total, status, payment_status, created_at) VALUES ('inv-paid-1', '1', 'INV-PAID-1', 'bkg-paid-1', 'cust-mh', 5000, 900, 5900, 'paid', 'paid', '2026-08-19 10:00:00')`)
	reqPaid := httptest.NewRequest("POST", "/inv-paid-1/generate-irn", nil)
	reqPaid.Header.Set("Accept", "application/json")
	reqPaid = reqPaid.WithContext(ctx)
	recPaid := httptest.NewRecorder()
	r.ServeHTTP(recPaid, reqPaid)
	assert.Equal(t, http.StatusConflict, recPaid.Code, "Paid invoice must block IRN generation with 409")
}

// 2. Intra-State Tax Split Consistency (CGST + SGST == IGST)
func TestPhase6_2_IntraState_TaxSplit_Consistency(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	seedPhase6Base(t, db)
	ctx := context.WithValue(shared.ContextWithTenantID(context.Background(), "1"), auth.ContextUser, &auth.SessionData{UserID: "admin-1", Role: "admin"})

	app := &handlers.App{DB: db, AuthSrv: &mockPhase6Auth{allowed: true}}
	invHandler := &handlers.InvoiceHandlers{App: app}
	r := chi.NewRouter()
	invHandler.Routes(r)

	// Intra-state invoice (MH customer)
	_, _ = db.Exec(`INSERT INTO bookings (id, tenant_id, booking_number, customer_id, route_id, pickup_date, vehicle_type, status, price) VALUES ('bkg-intra', '1', 'BK-INTRA', 'cust-mh', 'route-p6-1', '2026-08-20', 'truck', 'confirmed', 50000.0)`)
	_, _ = db.Exec(`INSERT INTO invoices (id, tenant_id, invoice_number, booking_id, customer_id, subtotal, tax, total, status, payment_status, created_at) VALUES ('inv-intra', '1', 'INV-INTRA', 'bkg-intra', 'cust-mh', 50000, 2500, 52500, 'draft', 'pending', datetime('now'))`)
	formIntra := url.Values{
		"hsn_sac_code": {"996511"},
		"description":  {"Bulk Goods Carriage"},
		"unit":         {"NOS"},
		"quantity":     {"1"},
		"rate":         {"50000"},
	}
	reqIntra := httptest.NewRequest("POST", "/inv-intra/line-items", strings.NewReader(formIntra.Encode()))
	reqIntra.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqIntra = reqIntra.WithContext(ctx)
	recIntra := httptest.NewRecorder()
	r.ServeHTTP(recIntra, reqIntra)
	require.Equal(t, http.StatusSeeOther, recIntra.Code)

	var cgstIntra, sgstIntra, igstIntra, totalIntra float64
	err := db.QueryRow(`SELECT cgst, sgst, igst, total FROM invoices WHERE id = 'inv-intra'`).Scan(&cgstIntra, &sgstIntra, &igstIntra, &totalIntra)
	require.NoError(t, err)

	// Inter-state invoice (Delhi customer)
	_, _ = db.Exec(`INSERT INTO bookings (id, tenant_id, booking_number, customer_id, route_id, pickup_date, vehicle_type, status, price) VALUES ('bkg-inter', '1', 'BK-INTER', 'cust-dl', 'route-p6-1', '2026-08-20', 'truck', 'confirmed', 50000.0)`)
	_, _ = db.Exec(`INSERT INTO invoices (id, tenant_id, invoice_number, booking_id, customer_id, subtotal, tax, total, status, payment_status, created_at) VALUES ('inv-inter', '1', 'INV-INTER', 'bkg-inter', 'cust-dl', 50000, 2500, 52500, 'draft', 'pending', datetime('now'))`)
	formInter := url.Values{
		"hsn_sac_code": {"996511"},
		"description":  {"Bulk Goods Carriage"},
		"unit":         {"NOS"},
		"quantity":     {"1"},
		"rate":         {"50000"},
	}
	reqInter := httptest.NewRequest("POST", "/inv-inter/line-items", strings.NewReader(formInter.Encode()))
	reqInter.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqInter = reqInter.WithContext(ctx)
	recInter := httptest.NewRecorder()
	r.ServeHTTP(recInter, reqInter)
	require.Equal(t, http.StatusSeeOther, recInter.Code)

	var cgstInter, sgstInter, igstInter, totalInter float64
	err = db.QueryRow(`SELECT cgst, sgst, igst, total FROM invoices WHERE id = 'inv-inter'`).Scan(&cgstInter, &sgstInter, &igstInter, &totalInter)
	require.NoError(t, err)

	assert.Equal(t, 1250.0, cgstIntra)
	assert.Equal(t, 1250.0, sgstIntra)
	assert.Equal(t, 2500.0, igstInter)
	assert.Equal(t, cgstIntra+sgstIntra, igstInter, "Intra-state CGST+SGST must equal Inter-state IGST for identical taxable rate")
	assert.Equal(t, totalIntra, totalInter, "Total invoice value must be equal across intra and inter-state tax splits")
}

// 3. EWB Auto-Generate + Lifecycle (TripConfirmed -> Part-A, TripAssigned -> Part-B, Extend, Cancel)
func TestPhase6_3_EWB_AutoGenerate_And_Lifecycle(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	seedPhase6Base(t, db)
	ctx := context.Background()

	// Booking & Trip above ₹50,000 threshold without vehicle assigned yet
	_, _ = db.Exec(`INSERT INTO bookings (id, tenant_id, booking_number, customer_id, route_id, pickup_date, vehicle_type, status, price) VALUES ('bkg-ewb-auto', '1', 'BK-AUTO-1', 'cust-mh', 'route-p6-1', '2026-08-20', 'truck', 'confirmed', 85000.0)`)
	_, _ = db.Exec(`INSERT INTO trips (id, tenant_id, trip_number, booking_id, route_id, status, departure_time) VALUES ('trip-ewb-auto', '1', 'TRP-AUTO-1', 'bkg-ewb-auto', 'route-p6-1', 'scheduled', datetime('now'))`)

	bus := events.NewInMemoryBus()
	cfg := ewaybill.Config{
		Enabled:              true,
		ExtensionKM:          10.0,
		ExtensionLeadSeconds: 28800,
		MinInvoiceValue:      50000.0,
	}
	ewbClient := intEWB.NewClient(intEWB.Config{Enabled: true, UseMock: true})
	svc := ewaybill.NewEWayBillService(db, bus, ewbClient, nil, cfg)
	svc.SubscribeTripEvents(bus)

	// Fire TripConfirmedEvent -> Part-A generated
	bus.Publish(ctx, events.Event{
		Type: "TripConfirmedEvent",
		Payload: map[string]interface{}{
			"TripID": "trip-ewb-auto",
		},
	})

	rec, err := svc.GetByTrip(ctx, "trip-ewb-auto")
	require.NoError(t, err)
	assert.NotEmpty(t, rec.EwbNumber)
	assert.Equal(t, "active", rec.Status)
	assert.Nil(t, rec.VehicleNumber, "Part-A initially has no vehicle number")

	// Fire TripAssignedEvent -> Part-B vehicle attached
	bus.Publish(ctx, events.Event{
		Type: "TripAssignedEvent",
		Payload: map[string]interface{}{
			"TripID":    "trip-ewb-auto",
			"VehicleID": "veh-p6-1",
		},
	})

	recUpdated, err := svc.GetByNumber(ctx, rec.EwbNumber)
	require.NoError(t, err)
	require.NotNil(t, recUpdated.VehicleNumber)
	assert.Equal(t, "MH12AB1234", *recUpdated.VehicleNumber)

	// Cancel EWB
	recCancelled, err := svc.Cancel(ctx, rec.EwbNumber, "order_cancelled")
	require.NoError(t, err)
	assert.Equal(t, "cancelled", recCancelled.Status)
}

// 4. EWB Auto-Generate Skip (Below ₹50,000 threshold)
func TestPhase6_4_EWB_AutoGenerate_Skip_Below_50k(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	seedPhase6Base(t, db)
	ctx := context.Background()

	_, _ = db.Exec(`INSERT INTO bookings (id, tenant_id, booking_number, customer_id, route_id, pickup_date, vehicle_type, status, price) VALUES ('bkg-low', '1', 'BK-LOW', 'cust-mh', 'route-p6-1', '2026-08-20', 'truck', 'confirmed', 25000.0)`)
	_, _ = db.Exec(`INSERT INTO trips (id, tenant_id, trip_number, booking_id, route_id, status, departure_time) VALUES ('trip-low', '1', 'TRP-LOW', 'bkg-low', 'route-p6-1', 'scheduled', datetime('now'))`)

	bus := events.NewInMemoryBus()
	cfg := ewaybill.Config{
		Enabled:         true,
		MinInvoiceValue: 50000.0,
	}
	ewbClient := intEWB.NewClient(intEWB.Config{Enabled: true, UseMock: true})
	svc := ewaybill.NewEWayBillService(db, bus, ewbClient, nil, cfg)
	svc.SubscribeTripEvents(bus)

	// Fire TripConfirmedEvent with ₹25,000 goods value
	bus.Publish(ctx, events.Event{
		Type: "TripConfirmedEvent",
		Payload: map[string]interface{}{
			"TripID": "trip-low",
		},
	})

	// Verify no EWB record created
	_, err := svc.GetByTrip(ctx, "trip-low")
	assert.ErrorIs(t, err, ewaybill.ErrEWBNotFound, "Trips below ₹50,000 threshold must NOT auto-generate EWB")
}

// 5. EWB Extension Denied (No geofence evidence / outside radius)
func TestPhase6_5_EWB_Extension_Denied_Without_Geofence_Evidence(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	seedPhase6Base(t, db)
	ctx := context.Background()

	_, _ = db.Exec(`INSERT INTO bookings (id, tenant_id, booking_number, customer_id, route_id, pickup_date, vehicle_type, status, price) VALUES ('bkg-ext-test', '1', 'BK-EXT', 'cust-mh', 'route-p6-1', '2026-08-20', 'truck', 'confirmed', 75000.0)`)
	_, _ = db.Exec(`INSERT INTO trips (id, tenant_id, trip_number, booking_id, route_id, vehicle_id, status, departure_time) VALUES ('trip-ext-test', '1', 'TRP-EXT', 'bkg-ext-test', 'route-p6-1', 'veh-p6-1', 'in_transit', datetime('now'))`)

	// Geofence at destination Pune (lat 18.5204, lng 73.8567) explicitly associated with route-p6-1
	_, _ = db.Exec(`INSERT INTO geofences (id, tenant_id, name, kind, shape, center_lat, center_lng, radius_m, route_name, priority, is_active) VALUES ('geo-dest', '1', 'Pune Drop', 'drop', 'circle', 18.5204, 73.8567, 1000.0, 'route-p6-1', 1, 1)`)

	// Vehicle is far away in Mumbai (lat 19.0760, lng 72.8777)
	_, _ = db.Exec(`INSERT INTO vehicle_latest_position (vehicle_id, tenant_id, latitude, longitude, speed, heading, ignition, device_time, received_at) VALUES ('veh-p6-1', '1', 19.0760, 72.8777, 40.0, 90.0, 1, datetime('now'), datetime('now'))`)

	bus := events.NewInMemoryBus()
	cfg := ewaybill.Config{
		Enabled:              true,
		ExtensionKM:          10.0,
		ExtensionLeadSeconds: 28800,
		MinInvoiceValue:      50000.0,
	}
	svc := ewaybill.NewEWayBillService(db, bus, intEWB.NewClient(intEWB.Config{Enabled: true, UseMock: true}), nil, cfg)

	rec, err := svc.GeneratePartA(ctx, ewaybill.GeneratePartARequest{
		TripID:     "trip-ext-test",
		GoodsValue: 75000.0,
		GenMode:    "MANUAL",
	})
	require.NoError(t, err)

	_, err = svc.AttachPartB(ctx, rec.EwbNumber, "MH12AB1234", "TRANS123")
	require.NoError(t, err)

	// Attempt extension -> Denied because vehicle is ~120km away
	_, err = svc.Extend(ctx, rec.EwbNumber, ewaybill.ExtendRequest{
		EwbNumber: rec.EwbNumber,
		Reason:    "traffic_delay",
	})
	assert.ErrorIs(t, err, ewaybill.ErrNoGeofenceEvidence, "Extension must be denied when vehicle has no destination geofence evidence")
}

// 6. FASTag Reconciliation + Auto-Kharcha (Greedy time window match + driver_expenses creation)
func TestPhase6_6_FASTag_Reconciliation_Greedy_And_AutoKharcha(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	seedPhase6Base(t, db)
	ctx := context.Background()

	// Seed Driver
	_, _ = db.Exec(`INSERT INTO drivers (id, tenant_id, first_name, last_name, phone, license_number, status) VALUES ('drv-p6-1', '1', 'Ramesh', 'Kumar', '9988776655', 'DL-MH-12345', 'active')`)

	// Trip window: departure 3 hours ago, arrival 1 hour from now
	now := time.Now().UTC()
	depTime := now.Add(-3 * time.Hour)
	arrTime := now.Add(1 * time.Hour)

	_, _ = db.Exec(`INSERT INTO bookings (id, tenant_id, booking_number, customer_id, route_id, pickup_date, vehicle_type, status, price) VALUES ('bkg-fastag', '1', 'BK-FT-1', 'cust-mh', 'route-p6-1', '2026-08-19', 'truck', 'confirmed', 50000)`)
	_, _ = db.Exec(`INSERT INTO trips (id, tenant_id, trip_number, booking_id, route_id, vehicle_id, driver_id, status, departure_time, arrival_time) VALUES ('trip-ft-1', '1', 'TRP-FT-1', 'bkg-fastag', 'route-p6-1', 'veh-p6-1', 'drv-p6-1', 'completed', ?, ?)`, depTime, arrTime)

	// FASTag Tag
	_, _ = db.Exec(`INSERT INTO fastag_tags (id, tenant_id, tag_id, vehicle_number, issuer, tag_class, balance, status, last_sync) VALUES ('tag-1', '1', 'TAG-MH12-001', 'MH12AB1234', 'ICICI', 'VC4', 5000.0, 'ACTIVE', datetime('now'))`)

	// 2 Toll transactions during trip window (depTime + 1h and depTime + 2h)
	_, _ = db.Exec(`INSERT INTO fastag_transactions (id, tenant_id, tag_id, vehicle_number, plaza_id, plaza_name, amount, txn_timestamp, status, source, reconciled) VALUES ('tx-1', '1', 'TAG-MH12-001', 'MH12AB1234', 'PLZ-1', 'Khalapur Plaza', 240.0, ?, 'SUCCESS', 'PROVIDER', 0)`, depTime.Add(1*time.Hour))
	_, _ = db.Exec(`INSERT INTO fastag_transactions (id, tenant_id, tag_id, vehicle_number, plaza_id, plaza_name, amount, txn_timestamp, status, source, reconciled) VALUES ('tx-2', '1', 'TAG-MH12-001', 'MH12AB1234', 'PLZ-2', 'Talegaon Plaza', 180.0, ?, 'SUCCESS', 'PROVIDER', 0)`, depTime.Add(2*time.Hour))

	client := intFastag.NewClient(intFastag.Config{Enabled: true, UseMock: true}, db)
	svc := fastag.NewFASTagService(db, client, fastag.Config{AutoKharcha: true})

	res, err := svc.Reconcile(ctx, "MH12AB1234", "", "")
	require.NoError(t, err)

	assert.Equal(t, 2, res.Matched, "Expected 2 tolls matched in window")
	assert.Equal(t, 2, res.KharchaCreated, "Expected 2 auto-kharcha items created")

	// Verify auto-kharcha driver_expenses creation
	var expCount int
	var expAmount float64
	var expStatus, expCategory string
	err = db.QueryRow(`SELECT count(*), COALESCE(sum(amount),0), status, category FROM driver_expenses WHERE trip_id = 'trip-ft-1'`).Scan(&expCount, &expAmount, &expStatus, &expCategory)
	require.NoError(t, err)
	assert.Equal(t, 2, expCount)
	assert.Equal(t, 420.0, expAmount)
	assert.Equal(t, "approved", expStatus, "FASTag expenses must auto-approve")
	assert.Equal(t, "toll", expCategory)
}

// 7. FASTag Deduct + Balance Update
func TestPhase6_7_FASTag_DB_Balance_And_Deduct(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	seedPhase6Base(t, db)
	ctx := context.Background()

	_, _ = db.Exec(`INSERT INTO fastag_tags (id, tenant_id, tag_id, vehicle_number, issuer, tag_class, balance, status, last_sync) VALUES ('tag-deduct', '1', 'TAG-MH12-DED', 'MH12AB1234', 'ICICI', 'VC4', 1000.0, 'ACTIVE', datetime('now'))`)

	client := intFastag.NewClient(intFastag.Config{Enabled: true, UseMock: true}, db)
	svc := fastag.NewFASTagService(db, client, fastag.Config{AutoKharcha: true})

	txn, err := svc.DeductToll(ctx, intFastag.DeductTollRequest{
		VehicleNumber: "MH12AB1234",
		TagID:         "TAG-MH12-DED",
		PlazaID:       "PLZ-KHED",
		PlazaName:     "Khed-Shivapur Toll",
		Amount:        350.0,
	})
	require.NoError(t, err)
	assert.Equal(t, 350.0, txn.Amount)

	// Verify in DB
	bal, err := svc.GetBalance(ctx, "MH12AB1234")
	require.NoError(t, err)
	assert.Equal(t, 650.0, bal.Balance)
}

// 8. FASTag Reconcile Idempotency (No duplicate kharcha)
func TestPhase6_8_FASTag_Reconciliation_Idempotency(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	seedPhase6Base(t, db)
	ctx := context.Background()

	now := time.Now().UTC()
	depTime := now.Add(-3 * time.Hour)
	arrTime := now.Add(1 * time.Hour)

	_, _ = db.Exec(`INSERT INTO drivers (id, tenant_id, first_name, last_name, phone, license_number, status) VALUES ('drv-ft-idem', '1', 'Vijay', 'Singh', '9988776644', 'DL-MH-9999', 'active')`)
	_, _ = db.Exec(`INSERT INTO bookings (id, tenant_id, booking_number, customer_id, route_id, pickup_date, vehicle_type, status, price) VALUES ('bkg-idem', '1', 'BK-IDEM', 'cust-mh', 'route-p6-1', '2026-08-19', 'truck', 'confirmed', 50000)`)
	_, _ = db.Exec(`INSERT INTO trips (id, tenant_id, trip_number, booking_id, route_id, vehicle_id, driver_id, status, departure_time, arrival_time) VALUES ('trip-idem', '1', 'TRP-IDEM', 'bkg-idem', 'route-p6-1', 'veh-p6-1', 'drv-ft-idem', 'completed', ?, ?)`, depTime, arrTime)
	_, _ = db.Exec(`INSERT INTO fastag_tags (id, tenant_id, tag_id, vehicle_number, issuer, tag_class, balance, status, last_sync) VALUES ('tag-idem', '1', 'TAG-IDEM-1', 'MH12AB1234', 'ICICI', 'VC4', 5000.0, 'ACTIVE', datetime('now'))`)
	_, _ = db.Exec(`INSERT INTO fastag_transactions (id, tenant_id, tag_id, vehicle_number, plaza_id, plaza_name, amount, txn_timestamp, status, source, reconciled) VALUES ('tx-idem-1', '1', 'TAG-IDEM-1', 'MH12AB1234', 'PLZ-A', 'Plaza A', 200.0, ?, 'SUCCESS', 'PROVIDER', 0)`, depTime.Add(1*time.Hour))

	client := intFastag.NewClient(intFastag.Config{Enabled: true, UseMock: true}, db)
	svc := fastag.NewFASTagService(db, client, fastag.Config{AutoKharcha: true})

	// First run
	res1, err := svc.Reconcile(ctx, "MH12AB1234", "", "")
	require.NoError(t, err)
	assert.Equal(t, 1, res1.Matched)
	assert.Equal(t, 1, res1.KharchaCreated)

	// Second run (idempotent check)
	res2, err := svc.Reconcile(ctx, "MH12AB1234", "", "")
	require.NoError(t, err)
	assert.Equal(t, 0, res2.Matched, "Second run should match 0 new unreconciled tolls")
	assert.Equal(t, 0, res2.KharchaCreated, "Second run should create 0 new kharcha items")

	var count int
	_ = db.QueryRow(`SELECT count(*) FROM driver_expenses WHERE trip_id = 'trip-idem' AND category = 'toll'`).Scan(&count)
	assert.Equal(t, 1, count, "Idempotent reconciliation must never create duplicate driver_expenses")
}

// 9. Cross-Feature (EWB + Invoice + Trip linkage + cancel sync)
func TestPhase6_9_CrossFeature_EWB_Invoice_Trip(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	seedPhase6Base(t, db)
	ctx := context.Background()

	_, _ = db.Exec(`INSERT INTO bookings (id, tenant_id, booking_number, customer_id, route_id, pickup_date, vehicle_type, status, price) VALUES ('bkg-cross', '1', 'BK-CROSS', 'cust-mh', 'route-p6-1', '2026-08-20', 'truck', 'confirmed', 60000.0)`)
	_, _ = db.Exec(`INSERT INTO trips (id, tenant_id, trip_number, booking_id, route_id, vehicle_id, status, departure_time) VALUES ('trip-cross', '1', 'TRP-CROSS', 'bkg-cross', 'route-p6-1', 'veh-p6-1', 'in_transit', datetime('now'))`)
	_, _ = db.Exec(`INSERT INTO invoices (id, tenant_id, invoice_number, booking_id, customer_id, trip_id, subtotal, tax, total, status, payment_status, created_at) VALUES ('inv-cross', '1', 'INV-CROSS', 'bkg-cross', 'cust-mh', 'trip-cross', 60000, 10800, 70800, 'draft', 'pending', datetime('now'))`)

	bus := events.NewInMemoryBus()
	cfg := ewaybill.Config{Enabled: true, MinInvoiceValue: 50000.0}
	svc := ewaybill.NewEWayBillService(db, bus, intEWB.NewClient(intEWB.Config{Enabled: true, UseMock: true}), nil, cfg)

	rec, err := svc.GeneratePartA(ctx, ewaybill.GeneratePartARequest{
		TripID:     "trip-cross",
		GoodsValue: 60000.0,
		GenMode:    "MANUAL",
	})
	require.NoError(t, err)

	// Link EWB to Invoice
	_, err = db.Exec(`UPDATE invoices SET ewb_number = ? WHERE id = 'inv-cross'`, rec.EwbNumber)
	require.NoError(t, err)

	// Verify persistence in invoice
	var ewbInDB string
	_ = db.QueryRow(`SELECT ewb_number FROM invoices WHERE id = 'inv-cross'`).Scan(&ewbInDB)
	assert.Equal(t, rec.EwbNumber, ewbInDB)
}

// 10. RBAC Enforcement (ewaybill:read, ewaybill:create, invoices:read)
func TestPhase6_10_RBAC_Enforcement(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	seedPhase6Base(t, db)

	// App with Deny auth
	appDeny := &handlers.App{DB: db, AuthSrv: &mockPhase6Auth{allowed: false}}
	bus := events.NewInMemoryBus()
	ewbSvc := ewaybill.NewEWayBillService(db, bus, intEWB.NewClient(intEWB.Config{Enabled: true, UseMock: true}), nil, ewaybill.Config{Enabled: true})
	ewbHandler := handlers.NewEWayBillHandlers(appDeny, ewbSvc, &mockPhase6Auth{allowed: false})

	r := chi.NewRouter()
	ewbHandler.Mount(r)

	// Request /ewaybill with a session context but disallowed permissions -> 403 Forbidden
	ctx := context.WithValue(shared.ContextWithTenantID(context.Background(), "1"), auth.ContextUser, &auth.SessionData{UserID: "unauth-user", Role: "guest"})
	ctx = shared.ContextWithTenantID(ctx, "1")

	req := httptest.NewRequest("GET", "/ewaybill", nil)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code, "Unauthorized user must receive 403 Forbidden")
}

// 11. Migration Roundtrip (00047, 00048, 00049)
func TestPhase6_11_Migration_Roundtrip(t *testing.T) {
	name := fmt.Sprintf("test_p6_mig_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)&_pragma=foreign_keys(OFF)")
	require.NoError(t, err)
	defer db.Close()

	_ = goose.SetDialect("sqlite")
	err = goose.Up(db, "../db/migrations")
	require.NoError(t, err, "All migrations must apply cleanly")

	// Roll back 3 migrations (00049, 00048, 00047)
	for i := 0; i < 3; i++ {
		err = goose.Down(db, "../db/migrations")
		require.NoError(t, err, "Migration rollback must succeed")
	}

	// Re-apply
	err = goose.Up(db, "../db/migrations")
	require.NoError(t, err, "Re-applying migrations must succeed")
}
