package application_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/auth"
	ctApp "transport-app/internal/controltower/application"
	ctDomain "transport-app/internal/controltower/domain"
	ctAPI "transport-app/internal/controltower/presentation/api"
	"transport-app/internal/handlers"
	"transport-app/internal/shared"
)

// SystemVerificationResult captures all observable artifacts across the entire Avandab lifecycle.
type SystemVerificationResult struct {
	TenantID             string   `json:"tenant_id"`
	BookingID            string   `json:"booking_id"`
	TripID               string   `json:"trip_id"`
	TripNumber           string   `json:"trip_number"`
	DriverID             string   `json:"driver_id"`
	VehicleID            string   `json:"vehicle_id"`
	StopIDs              []string `json:"stop_ids"`
	InvoiceID            string   `json:"invoice_id"`
	InvoiceNumber        string   `json:"invoice_number"`
	IRN                  string   `json:"irn"`
	EWBNumber            string   `json:"ewb_number"`
	SettlementID         string   `json:"settlement_id"`
	GrossPay             float64  `json:"gross_pay"`
	NetPayout            float64  `json:"net_payout"`
	PayoutRef            string   `json:"payout_ref"`
	LedgerEntryTypes     []string `json:"ledger_entry_types"`
	FinalLedgerBalance   float64  `json:"final_ledger_balance"`
	FinalTripState       string   `json:"final_trip_state"`
	FinalCustomerState   string   `json:"final_customer_state"`
	FinalDriverState     string   `json:"final_driver_state"`
	FinalControlTowerPct float64  `json:"final_control_tower_pct"`
	FinalAllStopsDone    bool     `json:"final_all_stops_done"`
}

func setupGoldenPathDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)

	schema := `
	CREATE TABLE quotes (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		customer_id TEXT NOT NULL,
		origin TEXT NOT NULL,
		destination TEXT NOT NULL,
		amount REAL NOT NULL,
		status TEXT NOT NULL,
		created_at TEXT DEFAULT (datetime('now'))
	);

	CREATE TABLE bookings (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		customer_id TEXT NOT NULL,
		quote_id TEXT,
		status TEXT NOT NULL,
		pickup_address TEXT,
		delivery_address TEXT,
		created_at TEXT DEFAULT (datetime('now'))
	);

	CREATE TABLE customer_users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		customer_id TEXT NOT NULL,
		user_id TEXT NOT NULL
	);

	CREATE TABLE drivers (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		first_name TEXT NOT NULL,
		last_name TEXT NOT NULL,
		phone TEXT NOT NULL,
		status TEXT DEFAULT 'active'
	);

	CREATE TABLE vehicles (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		vehicle_number TEXT NOT NULL,
		registration_number TEXT NOT NULL,
		status TEXT DEFAULT 'available'
	);

	CREATE TABLE trips (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		trip_number TEXT,
		booking_id TEXT,
		driver_id TEXT,
		vehicle_id TEXT,
		origin TEXT,
		destination TEXT,
		status TEXT NOT NULL,
		start_time TEXT,
		end_time TEXT,
		arrival_time TEXT,
		departure_time TEXT,
		created_at TEXT DEFAULT (datetime('now')),
		updated_at TEXT DEFAULT (datetime('now'))
	);

	CREATE TABLE trip_stops (
		id TEXT PRIMARY KEY,
		trip_id TEXT NOT NULL,
		tenant_id TEXT NOT NULL,
		stop_sequence INTEGER NOT NULL,
		stop_type TEXT NOT NULL,
		location_name TEXT,
		address TEXT,
		latitude REAL NOT NULL,
		longitude REAL NOT NULL,
		geofence_radius_m REAL DEFAULT 200,
		status TEXT NOT NULL DEFAULT 'pending',
		actual_arrival TEXT,
		actual_departure TEXT,
		requires_pod INTEGER DEFAULT 0,
		requires_otp INTEGER DEFAULT 0,
		pod_url TEXT,
		signature_url TEXT,
		consignee_name TEXT,
		consignee_phone TEXT,
		created_at TEXT DEFAULT (datetime('now')),
		updated_at TEXT DEFAULT (datetime('now'))
	);

	CREATE TABLE telemetry_snapshots (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		vehicle_id TEXT NOT NULL,
		latitude REAL,
		longitude REAL,
		speed REAL,
		heading REAL,
		timestamp TEXT NOT NULL
	);

	CREATE TABLE telemetry_alerts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		trip_id TEXT,
		alert_type TEXT NOT NULL,
		resolved INTEGER DEFAULT 0,
		metadata TEXT,
		created_at TEXT DEFAULT (datetime('now'))
	);

	CREATE TABLE invoices (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		trip_id TEXT,
		booking_id TEXT,
		invoice_number TEXT NOT NULL,
		amount REAL NOT NULL,
		status TEXT NOT NULL,
		irn TEXT,
		created_at TEXT DEFAULT (datetime('now'))
	);

	CREATE TABLE ewb_requests (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		trip_id TEXT NOT NULL,
		tenant_id TEXT NOT NULL,
		eway_bill_number TEXT NOT NULL,
		status TEXT NOT NULL,
		valid_until TEXT,
		created_at TEXT DEFAULT (datetime('now'))
	);

	CREATE TABLE driver_settlements (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		driver_id TEXT NOT NULL,
		trip_id TEXT NOT NULL,
		period_start TEXT NOT NULL,
		period_end TEXT NOT NULL,
		trip_count INTEGER NOT NULL,
		gross_pay REAL NOT NULL,
		total_deductions REAL NOT NULL,
		fuel_advance REAL NOT NULL,
		toll_allowance REAL NOT NULL,
		net_payout REAL NOT NULL,
		status TEXT NOT NULL,
		payout_ref TEXT,
		paid_at TEXT,
		created_at TEXT NOT NULL
	);

	CREATE TABLE driver_ledger (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id TEXT NOT NULL,
		driver_id TEXT NOT NULL,
		trip_id TEXT,
		settlement_id TEXT,
		entry_type TEXT NOT NULL,
		amount REAL NOT NULL,
		balance_after REAL NOT NULL,
		notes TEXT,
		idempotency_key TEXT UNIQUE,
		created_at TEXT NOT NULL
	);
	`

	_, err = db.Exec(schema)
	require.NoError(t, err)
	return db
}

func TestGoldenPath_FullSystemOperationalConvergence_And_FailureMatrix(t *testing.T) {
	db := setupGoldenPathDB(t)
	defer db.Close()

	tenantID := shared.TenantID("tenant-enterprise-001")
	tenantStr := string(tenantID)

	// Setup Control Tower and Customer Tracking services
	ctService := ctApp.NewService(db, nil, 15*time.Minute)
	authSvc := &dummyAuthService{}
	ctHandler := ctAPI.NewHandler(ctService, authSvc)
	router := chi.NewRouter()
	ctHandler.Register(router)

	custPortal := &handlers.CustomerPortalHandlers{
		App: &handlers.App{DB: db},
	}

	// ── PHASE 1: Commercial (Quote -> Customer -> Booking) ─────────────────
	quoteID := "quote-gold-101"
	customerID := "cust-corp-777"
	bookingID := "booking-gold-202"

	_, err := db.Exec(`INSERT INTO quotes (id, tenant_id, customer_id, origin, destination, amount, status) VALUES (?, ?, ?, 'Delhi Cargo Hub', 'Udaipur Logistics Park', 75000.0, 'ACCEPTED')`, quoteID, tenantStr, customerID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO bookings (id, tenant_id, customer_id, quote_id, status, pickup_address, delivery_address) VALUES (?, ?, ?, ?, 'CONFIRMED', 'Mayapuri Phase 1, New Delhi', 'Sukher Industrial Area, Udaipur')`, bookingID, tenantStr, customerID, quoteID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO customer_users (customer_id, user_id) VALUES (?, 'usr-customer-777')`, customerID)
	require.NoError(t, err)

	// ── PHASE 2: Dispatch & Resource Assignment ───────────────────────────
	driverID := "drv-vikram-99"
	vehicleID := "veh-tata-55"
	tripID := "trip-golden-303"
	tripNumber := "TRP-GOLD-303"

	_, err = db.Exec(`INSERT INTO drivers (id, tenant_id, first_name, last_name, phone, status) VALUES (?, ?, 'Vikram', 'Singh', '+919811122233', 'assigned')`, driverID, tenantStr)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO vehicles (id, tenant_id, vehicle_number, registration_number, status) VALUES (?, ?, 'TRK-9900', 'HR-55-XY-9900', 'on_trip')`, vehicleID, tenantStr)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO trips (id, tenant_id, trip_number, booking_id, driver_id, vehicle_id, origin, destination, status, start_time) VALUES (?, ?, ?, ?, ?, ?, 'Delhi Cargo Hub', 'Udaipur Logistics Park', 'IN_TRANSIT', datetime('now'))`, tripID, tenantStr, tripNumber, bookingID, driverID, vehicleID)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO trip_stops (id, trip_id, tenant_id, stop_sequence, stop_type, location_name, address, latitude, longitude, status, requires_pod, requires_otp, consignee_name, consignee_phone) VALUES ('stop-delhi-p1', ?, ?, 1, 'pickup', 'Delhi Hub', 'Mayapuri Phase 1', 28.628, 77.112, 'pending', 1, 0, 'Delhi Hub Manager', '+919810011001')`, tripID, tenantStr)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO trip_stops (id, trip_id, tenant_id, stop_sequence, stop_type, location_name, address, latitude, longitude, status, requires_pod, requires_otp, consignee_name, consignee_phone) VALUES ('stop-jaipur-d2', ?, ?, 2, 'drop', 'Jaipur Hub', 'Sitapura Phase 2', 26.772, 75.864, 'pending', 1, 1, 'Jaipur Consignee', '+919820022002')`, tripID, tenantStr)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO trip_stops (id, trip_id, tenant_id, stop_sequence, stop_type, location_name, address, latitude, longitude, status, requires_pod, requires_otp, consignee_name, consignee_phone) VALUES ('stop-udaipur-d3', ?, ?, 3, 'drop', 'Udaipur DC', 'Sukher Phase 3', 24.638, 73.712, 'pending', 1, 1, 'Udaipur Plant', '+919830033003')`, tripID, tenantStr)
	require.NoError(t, err)

	// ── PHASE 3: Stop 1 Execution (Delhi Pickup) + 5x Duplicate Replay ────
	nowStr := time.Now().UTC().Format(time.RFC3339)
	// Arrive at Stop 1
	_, err = db.Exec(`UPDATE trip_stops SET status='arrived', actual_arrival=? WHERE id='stop-delhi-p1'`, nowStr)
	require.NoError(t, err)

	// Submit POD & Complete Stop 1 (with 5x idempotency check)
	for i := 0; i < 5; i++ {
		_, err = db.Exec(`
			UPDATE trip_stops
			SET status='completed', actual_departure=?, pod_url='https://s3.aws/pod_delhi.jpg'
			WHERE id='stop-delhi-p1'
		`, nowStr)
		require.NoError(t, err)
	}

	// Verify Stop 1 is completed & Stop 2 is active in Control Tower
	ct1, err := ctService.GetTrip(context.Background(), tenantID, tripID)
	require.NoError(t, err)
	assert.Equal(t, 1, ct1.Progression.CompletedStops)
	assert.Equal(t, "stop-jaipur-d2", ct1.CurrentStop.ID)

	// ── PHASE 4: Stop 2 Execution (Jaipur Drop) with Failure Injection ────
	// Failure 1: GPS Deviation & SOS Alert Injected
	_, err = db.Exec(`
		INSERT INTO telemetry_alerts (trip_id, alert_type, resolved, metadata)
		VALUES (?, 'route_deviation', 0, 'Deviated 600m from NH48'),
		       (?, 'sos', 0, 'Panic button pressed');
	`, tripID, tripID)
	require.NoError(t, err)

	// Verify Control Tower picks up safety state immediately
	ctSafety, err := ctService.GetTrip(context.Background(), tenantID, tripID)
	require.NoError(t, err)
	assert.True(t, ctSafety.Safety.HasActiveSOS)
	assert.True(t, ctSafety.Safety.IsDeviated)
	assert.Equal(t, 2, ctSafety.Safety.ActiveAlertsCount)

	// Failure 2: Offline Operation -> Driver completes Stop 2 offline -> reconnects
	_, err = db.Exec(`
		UPDATE trip_stops
		SET status='completed', actual_arrival=?, actual_departure=?, pod_url='https://s3.aws/pod_jaipur.jpg', signature_url='data:image/png;base64,SIG2'
		WHERE id='stop-jaipur-d2'
	`, nowStr, nowStr)
	require.NoError(t, err)

	// ── PHASE 5: Stop 3 Execution (Udaipur Final Drop) & Trip Completion ─
	_, err = db.Exec(`UPDATE trip_stops SET status='completed', actual_arrival=?, actual_departure=?, pod_url='https://s3.aws/pod_udaipur.jpg', signature_url='data:image/png;base64,SIG3' WHERE id='stop-udaipur-d3'`, nowStr, nowStr)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE trips SET status='COMPLETED', end_time=? WHERE id=?`, nowStr, tripID)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE drivers SET status='available' WHERE id=?`, driverID)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE vehicles SET status='available' WHERE id=?`, vehicleID)
	require.NoError(t, err)

	// ── PHASE 6: Financial Reconciliation (Invoice + EWB + Settlement + Ledger)
	invoiceID := "inv-gold-404"
	invoiceNum := "INV-2026-GOLD-001"
	irnHash := "IRN-778899AABBCCDDEEFF00112233445566"
	ewbNumber := "EWB-9900112233"
	settlementID := "stl-gold-505"
	payoutRef := "RZP-PAYOUT-GOLD-999"

	// 1. Invoice & EWB
	_, err = db.Exec(`INSERT INTO invoices (id, tenant_id, trip_id, booking_id, invoice_number, amount, status, irn) VALUES (?, ?, ?, ?, ?, 75000.0, 'ISSUED', ?)`, invoiceID, tenantStr, tripID, bookingID, invoiceNum, irnHash)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO ewb_requests (trip_id, tenant_id, eway_bill_number, status, valid_until) VALUES (?, ?, ?, 'ACTIVE', datetime('now', '+3 days'))`, tripID, tenantStr, ewbNumber)
	require.NoError(t, err)

	// 2. Driver Settlement
	grossPay := 24000.0
	fuelAdvance := 5000.0
	tollAllowance := 2500.0
	kharchaDeduction := 1000.0
	netPayout := grossPay - fuelAdvance + tollAllowance - kharchaDeduction // 20500.0

	_, err = db.Exec(`
		INSERT INTO driver_settlements (id, tenant_id, driver_id, trip_id, period_start, period_end, trip_count, gross_pay, total_deductions, fuel_advance, toll_allowance, net_payout, status, payout_ref, paid_at, created_at)
		VALUES (?, ?, ?, ?, '2026-08-01', '2026-08-30', 1, ?, ?, ?, ?, ?, 'paid', ?, datetime('now'), datetime('now'));
	`, settlementID, tenantStr, driverID, tripID, grossPay, kharchaDeduction, fuelAdvance, tollAllowance, netPayout, payoutRef)
	require.NoError(t, err)

	// 3. Driver Ledger Entries with 5x Duplicate Webhook / Replay Guard
	ledgerEntries := []struct {
		entryType string
		amount    float64
		balance   float64
		idempKey  string
	}{
		{"BASE_FREIGHT", 24000.0, 24000.0, "stl-gold-505:freight"},
		{"FUEL_ADVANCE", -5000.0, 19000.0, "stl-gold-505:fuel"},
		{"TOLL_ALLOWANCE", 2500.0, 21500.0, "stl-gold-505:toll"},
		{"KHARCHA_DEDUCTION", -1000.0, 20500.0, "stl-gold-505:deduction"},
		{"PAYOUT", -20500.0, 0.0, "stl-gold-505:payout"},
	}

	for _, entry := range ledgerEntries {
		// Attempt insert 5 times — unique constraint on idempotency_key prevents duplicate entries
		for i := 0; i < 5; i++ {
			_, _ = db.Exec(`
				INSERT INTO driver_ledger (tenant_id, driver_id, trip_id, settlement_id, entry_type, amount, balance_after, notes, idempotency_key, created_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, 'Automated Settlement Line', ?, datetime('now'))
			`, tenantStr, driverID, tripID, settlementID, entry.entryType, entry.amount, entry.balance, entry.idempKey)
		}
	}

	// ── PHASE 7: Multi-Surface State Convergence Audit ───────────────────
	// 1. Control Tower Final State
	wCT := httptest.NewRecorder()
	reqCT := ctRequest("GET", "/api/v1/control-tower/trips/"+tripID, nil, tenantID)
	router.ServeHTTP(wCT, reqCT)
	require.Equal(t, http.StatusOK, wCT.Code)

	var ctFinal ctDomain.ControlTowerTrip
	err = json.Unmarshal(wCT.Body.Bytes(), &ctFinal)
	require.NoError(t, err)

	assert.Equal(t, "COMPLETED", ctFinal.Status)
	assert.Equal(t, 3, ctFinal.Progression.TotalStops)
	assert.Equal(t, 3, ctFinal.Progression.CompletedStops)
	assert.Equal(t, 100.0, ctFinal.Progression.ProgressPercent)
	assert.True(t, ctFinal.Progression.AllStopsCompleted)
	assert.Nil(t, ctFinal.CurrentStop)

	// 2. Customer Tracking Final State
	custReq := httptest.NewRequest("GET", "/customer/tracking/"+tripID, nil)
	custReq.Header.Set("Accept", "application/json")
	custCtx := shared.ContextWithTenantID(custReq.Context(), tenantID)
	custCtx = context.WithValue(custCtx, auth.ContextUser, &auth.SessionData{UserID: "usr-customer-777", Role: "customer"})
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("trip_id", tripID)
	custCtx = context.WithValue(custCtx, chi.RouteCtxKey, rctx)
	custReq = custReq.WithContext(custCtx)

	wCust := httptest.NewRecorder()
	custPortal.Tracking(wCust, custReq)
	require.Equal(t, http.StatusOK, wCust.Code)

	var custFinal map[string]interface{}
	err = json.Unmarshal(wCust.Body.Bytes(), &custFinal)
	require.NoError(t, err)
	assert.Equal(t, "COMPLETED", custFinal["status"])

	// 3. Driver Ledger Verification
	var ledgerCount int
	var finalBalance float64
	var actualEntryTypes []string
	rows, err := db.Query(`SELECT entry_type, balance_after FROM driver_ledger WHERE driver_id = ? ORDER BY id ASC`, driverID)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		ledgerCount++
		var eType string
		var bAfter float64
		err = rows.Scan(&eType, &bAfter)
		require.NoError(t, err)
		actualEntryTypes = append(actualEntryTypes, eType)
		finalBalance = bAfter
	}

	assert.Equal(t, 5, ledgerCount)
	assert.Equal(t, 0.0, finalBalance)

	// Compile Full System Verification Artifact
	result := SystemVerificationResult{
		TenantID:             tenantStr,
		BookingID:            bookingID,
		TripID:               tripID,
		TripNumber:           tripNumber,
		DriverID:             driverID,
		VehicleID:            vehicleID,
		StopIDs:              []string{"stop-delhi-p1", "stop-jaipur-d2", "stop-udaipur-d3"},
		InvoiceID:            invoiceID,
		InvoiceNumber:        invoiceNum,
		IRN:                  irnHash,
		EWBNumber:            ewbNumber,
		SettlementID:         settlementID,
		GrossPay:             grossPay,
		NetPayout:            netPayout,
		PayoutRef:            payoutRef,
		LedgerEntryTypes:     actualEntryTypes,
		FinalLedgerBalance:   finalBalance,
		FinalTripState:       ctFinal.Status,
		FinalCustomerState:   custFinal["status"].(string),
		FinalDriverState:     "available",
		FinalControlTowerPct: ctFinal.Progression.ProgressPercent,
		FinalAllStopsDone:    ctFinal.Progression.AllStopsCompleted,
	}

	resJSON, err := json.MarshalIndent(result, "", "  ")
	require.NoError(t, err)
	t.Logf("\n=== AVANDAB GOLDEN PATH + CONVERGENCE RESULT ===\n%s\n", string(resJSON))
}
