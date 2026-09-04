package ewaybill_test

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"

	_ "modernc.org/sqlite"

	"transport-app/internal/events"
	"transport-app/internal/ewaybill"
	intEWB "transport-app/internal/integration/ewaybill"
)

type mockRecordedBus struct {
	mu     sync.Mutex
	events []events.Event
	subs   map[string][]events.Handler
}

func newMockRecordedBus() *mockRecordedBus {
	return &mockRecordedBus{
		subs: make(map[string][]events.Handler),
	}
}

func (m *mockRecordedBus) Publish(ctx context.Context, e events.Event) {
	m.mu.Lock()
	m.events = append(m.events, e)
	handlers := m.subs[e.Type]
	m.mu.Unlock()

	for _, h := range handlers {
		_ = h(ctx, e)
	}
}

func (m *mockRecordedBus) Subscribe(eventType string, handler events.Handler) func() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subs[eventType] = append(m.subs[eventType], handler)
	return func() {}
}

func (m *mockRecordedBus) Count(eventType string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	cnt := 0
	for _, e := range m.events {
		if e.Type == eventType {
			cnt++
		}
	}
	return cnt
}

func setupP4TestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	schema := `
	CREATE TABLE IF NOT EXISTS company_settings (
		id INTEGER PRIMARY KEY,
		gst_number TEXT,
		state_code TEXT NOT NULL DEFAULT '27'
	);
	INSERT INTO company_settings (id, gst_number, state_code) VALUES (1, '27AABCU9603R1ZX', '27');

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

	CREATE TABLE IF NOT EXISTS routes (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL DEFAULT '1',
		source TEXT NOT NULL,
		destination TEXT NOT NULL,
		distance REAL NOT NULL DEFAULT 100,
		standard_fare REAL NOT NULL DEFAULT 5000
	);

	CREATE TABLE IF NOT EXISTS vehicles (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL DEFAULT '1',
		registration_number TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS bookings (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL DEFAULT '1',
		customer_id TEXT NOT NULL,
		route_id TEXT NOT NULL,
		price REAL NOT NULL DEFAULT 60000
	);

	CREATE TABLE IF NOT EXISTS trips (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL DEFAULT '1',
		trip_number TEXT NOT NULL,
		booking_id TEXT NOT NULL,
		route_id TEXT NOT NULL,
		vehicle_id TEXT,
		driver_id TEXT,
		status TEXT NOT NULL DEFAULT 'assigned',
		eway_bill_ref TEXT,
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
		payment_status TEXT NOT NULL DEFAULT 'pending',
		status TEXT NOT NULL DEFAULT 'draft',
		irn TEXT,
		irn_ack_no TEXT,
		irn_ack_date TEXT,
		signed_qr TEXT,
		ewb_number TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS eway_bills (
		id TEXT PRIMARY KEY,
		trip_id TEXT UNIQUE,
		ewb_number TEXT NOT NULL,
		status TEXT NOT NULL,
		generation_date DATETIME NOT NULL,
		valid_until DATETIME NOT NULL,
		from_place TEXT,
		from_state_code TEXT,
		to_place TEXT,
		to_state_code TEXT,
		goods_value REAL,
		distance INTEGER,
		doc_type TEXT,
		doc_no TEXT,
		doc_date TEXT,
		transporter_id TEXT,
		transporter_doc_no TEXT,
		qr_code TEXT,
		gen_mode TEXT,
		part_a_json TEXT,
		vehicle_number TEXT,
		extension_count INTEGER DEFAULT 0,
		cancel_reason TEXT,
		cancelled_at DATETIME,
		created_at DATETIME NOT NULL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS eway_bill_events (
		id TEXT PRIMARY KEY,
		ewb_number TEXT NOT NULL,
		trip_id TEXT,
		event_type TEXT NOT NULL,
		payload TEXT,
		created_by TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("schema setup: %v", err)
	}
	return db
}

func TestP4_MultiTenantEWayBillIsolation(t *testing.T) {
	db := setupP4TestDB(t)
	defer db.Close()

	bus := newMockRecordedBus()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	client := intEWB.NewClient(intEWB.Config{Enabled: true, UseMock: true})
	svc := ewaybill.NewEWayBillService(db, bus, client, logger, ewaybill.Config{Enabled: true, MinInvoiceValue: 50000})
	svc.SubscribeTripEvents(bus)

	// Tenant A: auto-generate disabled
	_, _ = db.Exec(`INSERT INTO company_config (tenant_id, key, value) VALUES ('tenant_A', 'ewaybill_auto_generate', 'false')`)
	// Tenant B: auto-generate enabled
	_, _ = db.Exec(`INSERT INTO company_config (tenant_id, key, value) VALUES ('tenant_B', 'ewaybill_auto_generate', 'true')`)

	// Seed Trip for Tenant A
	_, _ = db.Exec(`INSERT INTO customers (id, name, gst) VALUES ('cust_A', 'Cust A', '27AAAC0001A1Z1')`)
	_, _ = db.Exec(`INSERT INTO routes (id, tenant_id, source, destination, distance, standard_fare) VALUES ('rt_A', 'tenant_A', 'Mumbai', 'Pune', 150, 5000)`)
	_, _ = db.Exec(`INSERT INTO bookings (id, tenant_id, customer_id, route_id, price) VALUES ('bk_A', 'tenant_A', 'cust_A', 'rt_A', 75000)`)
	_, _ = db.Exec(`INSERT INTO trips (id, tenant_id, trip_number, booking_id, route_id, status) VALUES ('trip_A', 'tenant_A', 'TRP-A-1', 'bk_A', 'rt_A', 'started')`)

	// Seed Trip for Tenant B
	_, _ = db.Exec(`INSERT INTO customers (id, name, gst) VALUES ('cust_B', 'Cust B', '27AAAC0002B1Z2')`)
	_, _ = db.Exec(`INSERT INTO routes (id, tenant_id, source, destination, distance, standard_fare) VALUES ('rt_B', 'tenant_B', 'Delhi', 'Jaipur', 250, 8000)`)
	_, _ = db.Exec(`INSERT INTO bookings (id, tenant_id, customer_id, route_id, price) VALUES ('bk_B', 'tenant_B', 'cust_B', 'rt_B', 85000)`)
	_, _ = db.Exec(`INSERT INTO trips (id, tenant_id, trip_number, booking_id, route_id, status) VALUES ('trip_B', 'tenant_B', 'TRP-B-1', 'bk_B', 'rt_B', 'started')`)

	// Trigger TripConfirmed for Tenant A
	bus.Publish(context.Background(), events.Event{
		Type: "TripConfirmedEvent",
		Payload: map[string]interface{}{
			"trip_id":   "trip_A",
			"tenant_id": "tenant_A",
		},
	})

	// Trigger TripConfirmed for Tenant B
	bus.Publish(context.Background(), events.Event{
		Type: "TripConfirmedEvent",
		Payload: map[string]interface{}{
			"trip_id":   "trip_B",
			"tenant_id": "tenant_B",
		},
	})

	var countA, countB int
	_ = db.QueryRow(`SELECT COUNT(*) FROM eway_bills WHERE trip_id = 'trip_A'`).Scan(&countA)
	_ = db.QueryRow(`SELECT COUNT(*) FROM eway_bills WHERE trip_id = 'trip_B'`).Scan(&countB)

	if countA != 0 {
		t.Fatalf("Tenant A disabled ewaybill_auto_generate; expected 0 eway_bills, got %d", countA)
	}
	if countB != 1 {
		t.Fatalf("Tenant B enabled ewaybill_auto_generate; expected 1 eway_bill, got %d", countB)
	}
}

func TestP4_BiDirectionalInvoiceEWBReconciliationAnd5xReplay(t *testing.T) {
	db := setupP4TestDB(t)
	defer db.Close()

	bus := newMockRecordedBus()
	client := intEWB.NewClient(intEWB.Config{Enabled: true, UseMock: true})
	svc := ewaybill.NewEWayBillService(db, bus, client, slog.Default(), ewaybill.Config{Enabled: true, MinInvoiceValue: 50000})
	svc.SubscribeTripEvents(bus)

	tenantID := "tenant_t1"
	tripID := "trip_reconcile_001"
	invoiceID := "inv_001"
	invoiceNum := "INV-2026-001"

	// Seed customer, route, booking, trip, invoice
	_, _ = db.Exec(`INSERT INTO customers (id, name, gst) VALUES ('cust_1', 'Test Customer', '27AAAC0001A1Z1')`)
	_, _ = db.Exec(`INSERT INTO routes (id, tenant_id, source, destination, distance, standard_fare) VALUES ('rt_1', ?, 'Mumbai', 'Pune', 150, 5000)`, tenantID)
	_, _ = db.Exec(`INSERT INTO vehicles (id, tenant_id, registration_number) VALUES ('veh_1', ?, 'MH12AB1234')`, tenantID)
	_, _ = db.Exec(`INSERT INTO bookings (id, tenant_id, customer_id, route_id, price) VALUES ('bk_1', ?, 'cust_1', 'rt_1', 90000)`, tenantID)
	_, _ = db.Exec(`INSERT INTO trips (id, tenant_id, trip_number, booking_id, route_id, vehicle_id, status) VALUES (?, ?, 'TRP-REC-1', 'bk_1', 'rt_1', 'veh_1', 'started')`, tripID, tenantID)

	// Pre-create Invoice
	_, _ = db.Exec(`INSERT INTO invoices (id, tenant_id, invoice_number, trip_id, booking_id, customer_id, total, status)
		VALUES (?, ?, ?, ?, 'bk_1', 'cust_1', 90000, 'draft')`, invoiceID, tenantID, invoiceNum, tripID)

	// 1. Generate Part-A EWB
	rec, err := svc.GeneratePartA(context.Background(), ewaybill.GeneratePartARequest{
		TripID:     tripID,
		GoodsValue: 90000,
		GenMode:    "AUTO",
	})
	if err != nil {
		t.Fatalf("GeneratePartA failed: %v", err)
	}

	// Verify EWB was cross-linked to existing Invoice
	if rec.DocNo != invoiceNum {
		t.Fatalf("expected EWB DocNo to bind to existing invoice number %s, got %s", invoiceNum, rec.DocNo)
	}

	// Verify Invoice was cross-linked to generated EWB
	var linkedEWB sql.NullString
	_ = db.QueryRow(`SELECT ewb_number FROM invoices WHERE id = ?`, invoiceID).Scan(&linkedEWB)
	if !linkedEWB.Valid || linkedEWB.String != rec.EwbNumber {
		t.Fatalf("expected invoices.ewb_number to be %s, got %s", rec.EwbNumber, linkedEWB.String)
	}

	// 2. Invariant: 5x Replay idempotency
	for i := 0; i < 5; i++ {
		replayRec, err := svc.GeneratePartA(context.Background(), ewaybill.GeneratePartARequest{
			TripID:     tripID,
			GoodsValue: 90000,
			GenMode:    "AUTO",
		})
		if err != nil {
			t.Fatalf("replay %d failed: %v", i, err)
		}
		if replayRec.EwbNumber != rec.EwbNumber {
			t.Fatalf("replay must return identical ewb_number %s, got %s", rec.EwbNumber, replayRec.EwbNumber)
		}
	}

	var ewbRowCount int
	_ = db.QueryRow(`SELECT COUNT(*) FROM eway_bills WHERE trip_id = ?`, tripID).Scan(&ewbRowCount)
	if ewbRowCount != 1 {
		t.Fatalf("5x replay must produce exactly 1 eway_bills row, got %d", ewbRowCount)
	}
}

func TestP4_EWBExpiryMonitorAndAlertEngine(t *testing.T) {
	db := setupP4TestDB(t)
	defer db.Close()

	bus := newMockRecordedBus()
	client := intEWB.NewClient(intEWB.Config{Enabled: true, UseMock: true})
	svc := ewaybill.NewEWayBillService(db, bus, client, slog.Default(), ewaybill.Config{Enabled: true})

	// Seed an already expired EWB (valid_until = 1 hour ago).
	// trip_exp_1 is linked to a real trip so the alert carries its org.
	_, _ = db.Exec(`INSERT INTO trips (id, trip_number, booking_id, route_id, status, tenant_id) VALUES ('trip_exp_1', 'TR-EXP-1', 'bk_exp_1', 'rt_exp_1', 'in_transit', 'tenant-9')`)
	_, _ = db.Exec(`
		INSERT INTO eway_bills (id, trip_id, ewb_number, status, generation_date, valid_until, goods_value, created_at)
		VALUES ('ewb_exp_1', 'trip_exp_1', '999900001111', 'active', datetime('now', '-2 days'), datetime('now', '-1 hour'), 60000, datetime('now', '-2 days'))
	`)

	// Seed an expiring soon EWB (valid_until = 2 hours from now, within 8h lead)
	_, _ = db.Exec(`INSERT INTO trips (id, trip_number, booking_id, route_id, status, tenant_id) VALUES ('trip_exp_2', 'TR-EXP-2', 'bk_exp_2', 'rt_exp_2', 'in_transit', 'tenant-9')`)
	_, _ = db.Exec(`
		INSERT INTO eway_bills (id, trip_id, ewb_number, status, generation_date, valid_until, goods_value, created_at)
		VALUES ('ewb_exp_2', 'trip_exp_2', '999900002222', 'active', datetime('now', '-1 day'), datetime('now', '+2 hours'), 60000, datetime('now', '-1 day'))
	`)

	// Orphan EWB (trip deleted): status still transitions, but no alert may
	// be published for it — there is no org to attribute it to.
	_, _ = db.Exec(`
		INSERT INTO eway_bills (id, trip_id, ewb_number, status, generation_date, valid_until, goods_value, created_at)
		VALUES ('ewb_exp_3', 'trip_gone', '999900003333', 'active', datetime('now', '-2 days'), datetime('now', '-1 hour'), 60000, datetime('now', '-2 days'))
	`)

	monitor := ewaybill.NewMonitor(svc, ewaybill.Config{ExtensionLeadSeconds: 28800})
	monitor.Tick(context.Background())

	// Verify ewb_exp_1 transitioned to expired
	var status1 string
	_ = db.QueryRow(`SELECT status FROM eway_bills WHERE ewb_number = '999900001111'`).Scan(&status1)
	if status1 != "expired" {
		t.Fatalf("expected past-validity EWB to transition to 'expired', got '%s'", status1)
	}

	// Verify events dispatched to Alert Engine
	hasExpiredAlert := false
	hasExpiringSoonAlert := false
	for _, e := range bus.events {
		if e.Type == "AlertEvent" {
			if m, ok := e.Payload.(map[string]interface{}); ok {
				if m["alert_type"] == "ewb_expired" {
					hasExpiredAlert = true
					if m["tenant_id"] != "tenant-9" {
						t.Fatalf("expired alert attributed to %v, want tenant-9", m["tenant_id"])
					}
				}
				if m["alert_type"] == "ewb_expiring_soon" {
					hasExpiringSoonAlert = true
				}
			}
		}
	}

	if !hasExpiredAlert {
		t.Fatalf("expected ewb_expired AlertEvent to be dispatched")
	}
	if !hasExpiringSoonAlert {
		t.Fatalf("expected ewb_expiring_soon AlertEvent to be dispatched")
	}

	// Orphan transitions but never alerts.
	var status3 string
	_ = db.QueryRow(`SELECT status FROM eway_bills WHERE ewb_number = '999900003333'`).Scan(&status3)
	if status3 != "expired" {
		t.Fatalf("expected orphan EWB to transition to 'expired', got '%s'", status3)
	}
	for _, e := range bus.events {
		if e.Type == "AlertEvent" {
			if m, ok := e.Payload.(map[string]interface{}); ok {
				if details, _ := m["details"].(string); strings.Contains(details, "999900003333") {
					t.Fatalf("orphan EWB must not publish alerts")
				}
			}
		}
	}
}

func TestP4_TripDeliveryReconcilesEWBCompletion(t *testing.T) {
	db := setupP4TestDB(t)
	defer db.Close()

	bus := newMockRecordedBus()
	client := intEWB.NewClient(intEWB.Config{Enabled: true, UseMock: true})
	svc := ewaybill.NewEWayBillService(db, bus, client, slog.Default(), ewaybill.Config{Enabled: true})
	svc.SubscribeTripEvents(bus)

	tripID := "TRIP-DELIVERY-EWB-001"
	ewbNumber := "888800001234"

	_, _ = db.Exec(`
		INSERT INTO eway_bills (id, trip_id, ewb_number, status, generation_date, valid_until, goods_value, created_at)
		VALUES ('ewb_deliv_1', ?, ?, 'active', datetime('now', '-1 day'), datetime('now', '+1 day'), 70000, datetime('now', '-1 day'))
	`, tripID, ewbNumber)

	// Publish TripDeliveredEvent
	bus.Publish(context.Background(), events.Event{
		Type: "TripDeliveredEvent",
		Payload: map[string]interface{}{
			"trip_id":   tripID,
			"tenant_id": "1",
		},
	})

	var status string
	_ = db.QueryRow(`SELECT status FROM eway_bills WHERE ewb_number = ?`, ewbNumber).Scan(&status)
	if status != "delivered" {
		t.Fatalf("expected EWB status to be 'delivered' upon TripDeliveredEvent, got '%s'", status)
	}

	var eventType string
	_ = db.QueryRow(`SELECT event_type FROM eway_bill_events WHERE ewb_number = ?`, ewbNumber).Scan(&eventType)
	if eventType != "DELIVERED" {
		t.Fatalf("expected DELIVERED audit event in eway_bill_events, got '%s'", eventType)
	}
}
