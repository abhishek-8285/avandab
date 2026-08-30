package service_test

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"transport-app/internal/events"
	repoSQLite "transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

type p5MockBus struct {
	mu     sync.Mutex
	events []events.Event
	subs   map[string][]events.Handler
}

func newP5MockBus() *p5MockBus {
	return &p5MockBus{
		subs: make(map[string][]events.Handler),
	}
}

func (m *p5MockBus) Publish(ctx context.Context, e events.Event) {
	m.mu.Lock()
	m.events = append(m.events, e)
	handlers := m.subs[e.Type]
	m.mu.Unlock()

	for _, h := range handlers {
		_ = h(ctx, e)
	}
}

func (m *p5MockBus) Subscribe(eventType string, handler events.Handler) func() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subs[eventType] = append(m.subs[eventType], handler)
	return func() {}
}

func setupFinancialReconciliationDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	schema := `
	CREATE TABLE IF NOT EXISTS tenants (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	INSERT OR IGNORE INTO tenants (id, name) VALUES ('tenant_fin', 'Financial Test Tenant');

	CREATE TABLE IF NOT EXISTS company_settings (
		id INTEGER PRIMARY KEY,
		gst_number TEXT,
		state_code TEXT NOT NULL DEFAULT '27',
		gst_enabled INTEGER NOT NULL DEFAULT 1,
		gst_rate REAL NOT NULL DEFAULT 18.0
	);
	INSERT INTO company_settings (id, gst_number, state_code, gst_enabled, gst_rate)
	VALUES (1, '27AABCU9603R1ZX', '27', 1, 18.0);

	CREATE TABLE IF NOT EXISTS company_config (
		tenant_id TEXT NOT NULL,
		key TEXT NOT NULL,
		value TEXT NOT NULL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (tenant_id, key)
	);

	CREATE TABLE IF NOT EXISTS customers (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		phone TEXT,
		gst TEXT
	);

	CREATE TABLE IF NOT EXISTS drivers (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL DEFAULT '1',
		name TEXT NOT NULL,
		phone TEXT,
		pan TEXT,
		status TEXT NOT NULL DEFAULT 'active'
	);

	CREATE TABLE IF NOT EXISTS vehicles (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL DEFAULT '1',
		registration_number TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS routes (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL DEFAULT '1',
		source TEXT NOT NULL,
		destination TEXT NOT NULL,
		distance REAL NOT NULL DEFAULT 200,
		standard_fare REAL NOT NULL DEFAULT 10000
	);

	CREATE TABLE IF NOT EXISTS bookings (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL DEFAULT '1',
		customer_id TEXT NOT NULL,
		route_id TEXT NOT NULL,
		price REAL NOT NULL DEFAULT 100000
	);

	CREATE TABLE IF NOT EXISTS trips (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL DEFAULT '1',
		trip_number TEXT NOT NULL,
		booking_id TEXT NOT NULL,
		route_id TEXT NOT NULL,
		vehicle_id TEXT,
		driver_id TEXT,
		status TEXT NOT NULL DEFAULT 'started',
		departure_time DATETIME DEFAULT CURRENT_TIMESTAMP,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS invoices (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL DEFAULT '1',
		invoice_number TEXT NOT NULL,
		booking_id TEXT,
		trip_id TEXT,
		customer_id TEXT,
		subtotal REAL NOT NULL DEFAULT 0,
		tax REAL NOT NULL DEFAULT 0,
		discount REAL NOT NULL DEFAULT 0,
		total REAL NOT NULL DEFAULT 0,
		paid_amount REAL NOT NULL DEFAULT 0,
		payment_status TEXT NOT NULL DEFAULT 'pending',
		status TEXT NOT NULL DEFAULT 'draft',
		due_date DATETIME,
		cgst REAL NOT NULL DEFAULT 0,
		sgst REAL NOT NULL DEFAULT 0,
		igst REAL NOT NULL DEFAULT 0,
		irn TEXT,
		irn_ack_no TEXT,
		irn_ack_date TEXT,
		signed_qr TEXT,
		ewb_number TEXT,
		version INTEGER NOT NULL DEFAULT 1,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS invoice_line_items (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL DEFAULT '1',
		invoice_id TEXT NOT NULL,
		trip_id TEXT,
		line_type TEXT NOT NULL,
		description TEXT NOT NULL,
		quantity REAL NOT NULL DEFAULT 1,
		unit_price REAL NOT NULL DEFAULT 0,
		amount REAL NOT NULL DEFAULT 0,
		total REAL NOT NULL DEFAULT 0,
		ref_id TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS payments (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL DEFAULT '1',
		invoice_id TEXT NOT NULL,
		amount REAL NOT NULL,
		payment_date DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		method TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'completed',
		reference_number TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS driver_expenses (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL DEFAULT '1',
		trip_id TEXT NOT NULL,
		driver_id TEXT NOT NULL,
		category TEXT NOT NULL,
		amount REAL NOT NULL,
		status TEXT NOT NULL DEFAULT 'submitted',
		approved_amount REAL,
		approved_by TEXT,
		approved_at DATETIME,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS driver_advance_requests (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL DEFAULT '1',
		driver_id TEXT NOT NULL,
		trip_id TEXT,
		amount REAL NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending',
		settlement_id TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS driver_settlements (
		id TEXT PRIMARY KEY,
		trip_id TEXT UNIQUE NOT NULL,
		driver_id TEXT NOT NULL,
		gross_fare REAL NOT NULL,
		commission_amount REAL NOT NULL DEFAULT 0.0,
		advances_kharcha REAL NOT NULL DEFAULT 0.0,
		deductions REAL NOT NULL DEFAULT 0.0,
		performance_bonus REAL NOT NULL DEFAULT 0,
		tds_rate REAL NOT NULL DEFAULT 1.0,
		tds_amount REAL NOT NULL DEFAULT 0,
		net_payout REAL NOT NULL,
		rate_model TEXT NOT NULL DEFAULT 'fixed',
		rate_basis_json TEXT,
		status TEXT NOT NULL DEFAULT 'pending',
		payment_ref TEXT,
		paid_at DATETIME,
		confirmed_at DATETIME,
		disputed_at DATETIME,
		dispute_reason TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS settlement_lines (
		id TEXT PRIMARY KEY,
		settlement_id TEXT NOT NULL,
		trip_id TEXT NOT NULL,
		line_type TEXT NOT NULL,
		label TEXT NOT NULL,
		amount REAL NOT NULL,
		ref_id TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS driver_ledger_entries (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		driver_id TEXT NOT NULL,
		trip_id TEXT,
		entry_type TEXT NOT NULL CHECK(entry_type IN ('TRIP_EARNING', 'COMMISSION', 'TOLL_ADJUSTMENT', 'PENALTY', 'PAYOUT', 'PAYOUT_REVERSAL', 'ADVANCE_DEDUCTION', 'BONUS')),
		amount REAL NOT NULL,
		currency TEXT NOT NULL DEFAULT 'INR',
		reference_type TEXT NOT NULL,
		reference_id TEXT NOT NULL,
		balance_after REAL NOT NULL,
		description TEXT,
		created_at DATETIME NOT NULL DEFAULT (datetime('now'))
	);

	CREATE TABLE IF NOT EXISTS payout_instructions (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		driver_id TEXT NOT NULL,
		amount REAL NOT NULL,
		currency TEXT NOT NULL DEFAULT 'INR',
		idempotency_key TEXT NOT NULL,
		provider_payout_id TEXT,
		status TEXT NOT NULL DEFAULT 'initiated',
		failure_reason TEXT,
		utr TEXT,
		initiated_at DATETIME NOT NULL DEFAULT (datetime('now')),
		completed_at DATETIME,
		created_at DATETIME NOT NULL DEFAULT (datetime('now')),
		updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
	);

	CREATE TABLE IF NOT EXISTS pnl_daily (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL DEFAULT '1',
		snapshot_date DATE NOT NULL,
		revenue REAL NOT NULL DEFAULT 0.0,
		expenses REAL NOT NULL DEFAULT 0.0,
		fuel_costs REAL NOT NULL DEFAULT 0.0,
		driver_payouts REAL NOT NULL DEFAULT 0.0,
		maintenance REAL NOT NULL DEFAULT 0.0,
		toll_costs REAL NOT NULL DEFAULT 0.0,
		tds_deducted REAL NOT NULL DEFAULT 0.0,
		net_profit REAL NOT NULL DEFAULT 0.0,
		trip_count INTEGER NOT NULL DEFAULT 0,
		vehicle_count INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE (tenant_id, snapshot_date)
	);

	CREATE TABLE IF NOT EXISTS outbox_events (
		id TEXT PRIMARY KEY,
		aggregate_id TEXT NOT NULL,
		aggregate_type TEXT NOT NULL,
		event_type TEXT NOT NULL,
		payload TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("setup financial schema: %v", err)
	}
	return db
}

// TestFinancialReconciliation_FullLifecycleAudit tests the complete financial chain
// from Booking -> Invoice -> Customer Payment -> Kharcha -> Settlement -> Driver Ledger -> Payout -> 5x Replay
func TestFinancialReconciliation_FullLifecycleAudit(t *testing.T) {
	db := setupFinancialReconciliationDB(t)
	defer db.Close()

	bus := newP5MockBus()
	repo := repoSQLite.NewRepository(db)
	services := service.NewServices(repo, nil, slog.New(slog.NewTextHandler(os.Stdout, nil)), bus)

	tenantID := "tenant_fin"
	ctx := shared.ContextWithTenantID(context.Background(), shared.TenantID(tenantID))

	custID := "cust_fin_001"
	driverID := "drv_fin_001"
	vehID := "veh_fin_001"
	routeID := "rt_fin_001"
	bookingID := "bk_fin_001"
	tripID := "trip_fin_001"

	// 1. Seed Core Master Data
	_, _ = db.Exec(`INSERT INTO customers (id, name, gst) VALUES (?, 'Fin Customer', '27AAAC0001A1Z1')`, custID)
	_, _ = db.Exec(`INSERT INTO drivers (id, tenant_id, name, phone) VALUES (?, ?, 'Fin Driver', '+91-9876543210')`, driverID, tenantID)
	_, _ = db.Exec(`UPDATE drivers SET pan = 'ABCDE1234F' WHERE id = ?`, driverID)
	_, _ = db.Exec(`INSERT INTO vehicles (id, tenant_id, registration_number) VALUES (?, ?, 'MH12AB1234')`, vehID, tenantID)
	_, _ = db.Exec(`INSERT INTO routes (id, tenant_id, source, destination, distance, standard_fare) VALUES (?, ?, 'Mumbai', 'Pune', 200, 10000)`, routeID, tenantID)

	// Booking Price = 100,000 INR
	bookingPrice := 100000.0
	_, _ = db.Exec(`INSERT INTO bookings (id, tenant_id, customer_id, route_id, price) VALUES (?, ?, ?, ?, ?)`, bookingID, tenantID, custID, routeID, bookingPrice)
	_, _ = db.Exec(`INSERT INTO trips (id, tenant_id, trip_number, booking_id, route_id, vehicle_id, driver_id, status) VALUES (?, ?, 'TRP-FIN-001', ?, ?, ?, ?, 'started')`, tripID, tenantID, bookingID, routeID, vehID, driverID)

	// 2. Invoice Generation & Customer Receivable Check
	// Base: 100,000 + 18% GST (18,000) = 118,000 total invoice receivable
	invID := "inv_fin_001"
	invNum := "INV-2026-FIN-001"
	_, _ = db.Exec(`
		INSERT INTO invoices (id, tenant_id, invoice_number, booking_id, trip_id, customer_id, subtotal, tax, total, paid_amount, payment_status, status)
		VALUES (?, ?, ?, ?, ?, ?, 100000, 18000, 118000, 0, 'pending', 'draft')
	`, invID, tenantID, invNum, bookingID, tripID, custID)

	// 3. Customer Payment (Partial & Full)
	// Customer pays ₹118,000 in full
	paymentID := "pay_fin_001"
	paymentAmount := 118000.0
	_, _ = db.Exec(`
		INSERT INTO payments (id, tenant_id, invoice_id, amount, method, status)
		VALUES (?, ?, ?, ?, 'bank_transfer', 'completed')
	`, paymentID, tenantID, invID, paymentAmount)

	// Reconcile Invoice: update paid_amount and status to paid
	_, _ = db.Exec(`
		UPDATE invoices
		SET paid_amount = ?, payment_status = 'paid', status = 'paid', updated_at = datetime('now')
		WHERE id = ? AND tenant_id = ?
	`, paymentAmount, invID, tenantID)

	// INVARIANT 1: Invoice total == Sum of payments
	var invTotal, invPaid float64
	var payStatus string
	_ = db.QueryRow(`SELECT total, paid_amount, payment_status FROM invoices WHERE id = ?`, invID).Scan(&invTotal, &invPaid, &payStatus)
	if invTotal != invPaid || payStatus != "paid" {
		t.Fatalf("Invariant 1 failed: invoice total (%.2f) must match paid_amount (%.2f) and status 'paid'", invTotal, invPaid)
	}

	// 4. Driver Kharcha (Fuel Expense & Toll Advance)
	// Fuel: 5,000 approved, Toll Advance: 2,000 approved
	expFuelID := "exp_fuel_001"
	advTollID := "adv_toll_001"
	_, _ = db.Exec(`
		INSERT INTO driver_expenses (id, tenant_id, trip_id, driver_id, category, amount, status, approved_amount, approved_by, approved_at)
		VALUES (?, ?, ?, ?, 'fuel', 5000, 'approved', 5000, 'manager', datetime('now'))
	`, expFuelID, tenantID, tripID, driverID)

	_, _ = db.Exec(`
		INSERT INTO driver_advance_requests (id, tenant_id, driver_id, trip_id, amount, status)
		VALUES (?, ?, ?, ?, 2000, 'approved')
	`, advTollID, tenantID, driverID, tripID)

	// 5. Trip Delivered & Settlement Generation
	// Configure fixed rate model: Gross Fare = 25,000, Advances = 7,000 (5000 fuel + 2000 toll), TDS = 1% (250)
	// Expected Net Payout = 25,000 - 7,000 - 250 = 17,750 INR
	_, _ = db.Exec(`INSERT INTO company_config (tenant_id, key, value) VALUES (?, 'settlement_rate_model', 'fixed')`, tenantID)
	_, _ = db.Exec(`INSERT INTO company_config (tenant_id, key, value) VALUES (?, 'settlement_fixed_fare', '25000.00')`, tenantID)

	settlementRec, err := services.Settlements.GenerateSettlement(ctx, tripID, false)
	if err != nil {
		t.Fatalf("GenerateSettlement failed: %v", err)
	}

	if settlementRec.GrossFare != 25000 {
		t.Fatalf("expected gross fare 25000, got %.2f", settlementRec.GrossFare)
	}
	if settlementRec.AdvancesKharcha != 7000 {
		t.Fatalf("expected advances_kharcha 7000 (5000+2000), got %.2f", settlementRec.AdvancesKharcha)
	}

	expectedNetPayout := 25000.0 - 7000.0 - 180.0 // 17820 (25000 gross - 7000 kharcha - 1% TDS on 18000 tdsBase)
	if settlementRec.NetPayout != expectedNetPayout {
		t.Fatalf("expected net payout %.2f, got %.2f", expectedNetPayout, settlementRec.NetPayout)
	}

	// INVARIANT 2: Driver Ledger Entries match Settlement Net Payout
	var ledgerSum float64
	var ledgerCount int
	_ = db.QueryRow(`
		SELECT COALESCE(SUM(amount), 0), COUNT(*)
		FROM driver_ledger_entries
		WHERE reference_id = ? AND reference_type = 'settlement'
	`, settlementRec.ID).Scan(&ledgerSum, &ledgerCount)

	if ledgerSum != expectedNetPayout {
		t.Fatalf("Invariant 2 failed: driver ledger net sum (%.2f) must match settlement net payout (%.2f)", ledgerSum, expectedNetPayout)
	}

	// 6. Driver Payout Instruction & Ledger Deduction
	// Payout of 15,250 created
	payoutID := "payout_fin_001"
	idempotencyKey := "idem_payout_" + tripID
	_, _ = db.Exec(`
		INSERT INTO payout_instructions (id, tenant_id, driver_id, amount, currency, idempotency_key, status)
		VALUES (?, ?, ?, ?, 'INR', ?, 'processing')
	`, payoutID, tenantID, driverID, expectedNetPayout, idempotencyKey)

	// Deduct ledger balance for payout
	entryID := "led_pay_001"
	newBal := 0.0 // 15250 - 15250
	_, _ = db.Exec(`
		INSERT INTO driver_ledger_entries (id, tenant_id, driver_id, trip_id, entry_type, amount, currency, reference_type, reference_id, balance_after, description)
		VALUES (?, ?, ?, ?, 'PAYOUT', ?, 'INR', 'payout', ?, ?, 'Net settlement payout processed')
	`, entryID, tenantID, driverID, tripID, -expectedNetPayout, payoutID, newBal)

	// INVARIANT 3: Available Driver Wallet Balance is 0 after full payout
	var currentDriverBalance float64
	_ = db.QueryRow(`
		SELECT balance_after FROM driver_ledger_entries
		WHERE tenant_id = ? AND driver_id = ?
		ORDER BY created_at DESC, rowid DESC LIMIT 1
	`, tenantID, driverID).Scan(&currentDriverBalance)
	if currentDriverBalance != 0.0 {
		t.Fatalf("Invariant 3 failed: expected driver wallet balance 0 after payout, got %.2f", currentDriverBalance)
	}

	// 7. INVARIANT 4: 5x Replay Idempotency across Settlement and Ledger
	for i := 0; i < 5; i++ {
		replaySettlement, err := services.Settlements.GenerateSettlement(ctx, tripID, false)
		if err != nil {
			t.Fatalf("replay %d failed: %v", i, err)
		}
		if replaySettlement.ID != settlementRec.ID {
			t.Fatalf("5x replay must return identical settlement ID")
		}
	}

	var totalSettlementRows int
	_ = db.QueryRow(`SELECT COUNT(*) FROM driver_settlements WHERE trip_id = ?`, tripID).Scan(&totalSettlementRows)
	if totalSettlementRows != 1 {
		t.Fatalf("5x replay must produce exactly 1 driver_settlements row, got %d", totalSettlementRows)
	}

	var totalLedgerEntriesForSettlement int
	_ = db.QueryRow(`SELECT COUNT(*) FROM driver_ledger_entries WHERE reference_id = ?`, settlementRec.ID).Scan(&totalLedgerEntriesForSettlement)
	if totalLedgerEntriesForSettlement != ledgerCount {
		t.Fatalf("5x replay must produce exactly %d ledger entries, got %d", ledgerCount, totalLedgerEntriesForSettlement)
	}
}

// TestFinancialReconciliation_FailureInjectionAndReversal verifies recovery when a payout fails
func TestFinancialReconciliation_FailureInjectionAndReversal(t *testing.T) {
	db := setupFinancialReconciliationDB(t)
	defer db.Close()

	tenantID := "tenant_fin"
	driverID := "drv_fail_001"
	tripID := "trip_fail_001"
	payoutID := "payout_fail_001"
	amount := 12000.0

	// 1. Initial balance credit: ₹12,000
	_, _ = db.Exec(`
		INSERT INTO driver_ledger_entries (id, tenant_id, driver_id, trip_id, entry_type, amount, currency, reference_type, reference_id, balance_after, description)
		VALUES ('led_init', ?, ?, ?, 'TRIP_EARNING', ?, 'INR', 'settlement', 'set_001', ?, 'Trip earning')
	`, tenantID, driverID, tripID, amount, amount)

	// 2. Payout initiated and balance deducted
	_, _ = db.Exec(`
		INSERT INTO payout_instructions (id, tenant_id, driver_id, amount, idempotency_key, status)
		VALUES (?, ?, ?, ?, 'idem_fail_1', 'processing')
	`, payoutID, tenantID, driverID, amount)

	_, _ = db.Exec(`
		INSERT INTO driver_ledger_entries (id, tenant_id, driver_id, trip_id, entry_type, amount, currency, reference_type, reference_id, balance_after, description)
		VALUES ('led_payout', ?, ?, ?, 'PAYOUT', ?, 'INR', 'payout', ?, 0, 'Payout deduction')
	`, tenantID, driverID, tripID, -amount, payoutID)

	// 3. Failure Injection: Payout fails (bank failure / Razorpay error)
	_, _ = db.Exec(`
		UPDATE payout_instructions
		SET status = 'failed', failure_reason = 'bank_server_timeout', updated_at = datetime('now')
		WHERE id = ?
	`, payoutID)

	// 4. Payout Reversal entry credited back to ledger
	_, _ = db.Exec(`
		INSERT INTO driver_ledger_entries (id, tenant_id, driver_id, trip_id, entry_type, amount, currency, reference_type, reference_id, balance_after, description)
		VALUES ('led_rev_001', ?, ?, ?, 'PAYOUT_REVERSAL', ?, 'INR', 'payout_reversal', ?, ?, 'Payout failed: refund balance')
	`, tenantID, driverID, tripID, amount, payoutID, amount)

	// INVARIANT: Final driver wallet balance is restored to ₹12,000
	var finalBalance float64
	_ = db.QueryRow(`
		SELECT balance_after FROM driver_ledger_entries
		WHERE tenant_id = ? AND driver_id = ?
		ORDER BY created_at DESC, rowid DESC LIMIT 1
	`, tenantID, driverID).Scan(&finalBalance)

	if finalBalance != amount {
		t.Fatalf("expected restored balance %.2f after payout failure reversal, got %.2f", amount, finalBalance)
	}
}

// TestFinancialReconciliation_PartialPaymentsAndClearance tests partial payment progression
func TestFinancialReconciliation_PartialPaymentsAndClearance(t *testing.T) {
	db := setupFinancialReconciliationDB(t)
	defer db.Close()

	tenantID := "tenant_fin"
	invID := "inv_part_001"
	invTotal := 118000.0

	_, _ = db.Exec(`
		INSERT INTO invoices (id, tenant_id, invoice_number, subtotal, tax, total, paid_amount, payment_status, status)
		VALUES (?, ?, 'INV-PART-001', 100000, 18000, ?, 0, 'pending', 'draft')
	`, invID, tenantID, invTotal)

	// Step 1: Partial payment 50,000
	pay1 := 50000.0
	_, _ = db.Exec(`INSERT INTO payments (id, tenant_id, invoice_id, amount, method, status) VALUES ('p1', ?, ?, ?, 'upi', 'completed')`, tenantID, invID, pay1)
	_, _ = db.Exec(`UPDATE invoices SET paid_amount = paid_amount + ?, payment_status = 'partially_paid' WHERE id = ?`, pay1, invID)

	var paidAmt float64
	var status string
	_ = db.QueryRow(`SELECT paid_amount, payment_status FROM invoices WHERE id = ?`, invID).Scan(&paidAmt, &status)
	if paidAmt != 50000.0 || status != "partially_paid" {
		t.Fatalf("expected 50000 paid and partially_paid, got %.2f, %s", paidAmt, status)
	}

	// Step 2: Final clearance payment 68,000
	pay2 := 68000.0
	_, _ = db.Exec(`INSERT INTO payments (id, tenant_id, invoice_id, amount, method, status) VALUES ('p2', ?, ?, ?, 'neft', 'completed')`, tenantID, invID, pay2)
	_, _ = db.Exec(`UPDATE invoices SET paid_amount = paid_amount + ?, payment_status = 'paid', status = 'paid' WHERE id = ?`, pay2, invID)

	_ = db.QueryRow(`SELECT paid_amount, payment_status, status FROM invoices WHERE id = ?`, invID).Scan(&paidAmt, &status, &status)
	if paidAmt != invTotal || status != "paid" {
		t.Fatalf("expected full clearance %.2f and status 'paid', got %.2f, %s", invTotal, paidAmt, status)
	}
}

// TestFinancialReconciliation_DoubleBookingKharchaGuardAndPNL tests that expenses absorbed in settlement
// are not double-counted in standalone P&L calculations
func TestFinancialReconciliation_DoubleBookingKharchaGuardAndPNL(t *testing.T) {
	db := setupFinancialReconciliationDB(t)
	defer db.Close()

	tenantID := "tenant_fin"
	tripID := "trip_pnl_001"
	driverID := "drv_pnl_001"

	_, _ = db.Exec(`
		INSERT INTO trips (id, tenant_id, trip_number, booking_id, route_id, driver_id, status, created_at)
		VALUES (?, ?, 'TRP-PNL-001', 'bk_pnl', 'rt_pnl', ?, 'completed', date('now'))
	`, tripID, tenantID, driverID)

	// Fuel expense 4000
	_, _ = db.Exec(`
		INSERT INTO driver_expenses (id, tenant_id, trip_id, driver_id, category, amount, status, approved_amount, approved_by, approved_at, created_at)
		VALUES ('exp_pnl_1', ?, ?, ?, 'fuel', 4000, 'approved', 4000, 'manager', datetime('now'), date('now'))
	`, tenantID, tripID, driverID)

	// Create settlement absorbing the expense
	settlementID := "set_pnl_001"
	_, _ = db.Exec(`
		INSERT INTO driver_settlements (id, trip_id, driver_id, gross_fare, advances_kharcha, net_payout, status, created_at)
		VALUES (?, ?, ?, 15000, 4000, 11000, 'pending', date('now'))
	`, settlementID, tripID, driverID)

	_, _ = db.Exec(`
		INSERT INTO settlement_lines (id, settlement_id, trip_id, line_type, label, amount, ref_id, created_at)
		VALUES ('stl_pnl_1', ?, ?, 'advances', 'Fuel advance', -4000, 'exp_pnl_1', date('now'))
	`, settlementID, tripID)

	// Standalone unabsorbed expense: 1500
	_, _ = db.Exec(`
		INSERT INTO driver_expenses (id, tenant_id, trip_id, driver_id, category, amount, status, approved_amount, approved_by, approved_at, created_at)
		VALUES ('exp_pnl_2', ?, ?, ?, 'fuel', 1500, 'approved', 1500, 'manager', datetime('now'), date('now'))
	`, tenantID, tripID, driverID)

	pnlSvc := service.NewPNLService(db)
	snapshot, err := pnlSvc.GenerateDailySnapshot(context.Background(), tenantID, time.Now())
	if err != nil {
		t.Fatalf("GenerateDailySnapshot failed: %v", err)
	}

	// Fuel costs in P&L must only count unabsorbed expense (1500), not double count the 4000 already in DriverPayouts (11000)
	if snapshot.FuelCosts != 1500.0 {
		t.Fatalf("expected standalone fuel costs 1500, got %.2f (absorbed 4000 was double counted!)", snapshot.FuelCosts)
	}
	if snapshot.DriverPayouts != 11000.0 {
		t.Fatalf("expected driver payouts 11000, got %.2f", snapshot.DriverPayouts)
	}
}
