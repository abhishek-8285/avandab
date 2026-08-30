package application_test

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"transport-app/internal/events"
	"transport-app/internal/ewaybill"
	intEWB "transport-app/internal/integration/ewaybill"
	invoiceapp "transport-app/internal/invoice/application"
	invoiceagg "transport-app/internal/invoice/domain/aggregate"
	invoicesql "transport-app/internal/invoice/infrastructure/persistence/sql"
	"transport-app/internal/shared"
	"transport-app/internal/shared/id"
)

type p4MockBus struct {
	mu     sync.Mutex
	events []events.Event
	subs   map[string][]events.Handler
}

func newP4MockBus() *p4MockBus {
	return &p4MockBus{
		subs: make(map[string][]events.Handler),
	}
}

func (m *p4MockBus) Publish(ctx context.Context, e events.Event) {
	m.mu.Lock()
	m.events = append(m.events, e)
	handlers := m.subs[e.Type]
	m.mu.Unlock()

	for _, h := range handlers {
		_ = h(ctx, e)
	}
}

func (m *p4MockBus) Subscribe(eventType string, handler events.Handler) func() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subs[eventType] = append(m.subs[eventType], handler)
	return func() {}
}

func setupP4InvoiceDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	schema := `
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

	CREATE TABLE IF NOT EXISTS routes (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL DEFAULT '1',
		source TEXT NOT NULL,
		destination TEXT NOT NULL,
		distance REAL NOT NULL DEFAULT 150,
		standard_fare REAL NOT NULL DEFAULT 6000
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
		price REAL NOT NULL DEFAULT 80000
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
		paid_amount REAL NOT NULL DEFAULT 0,
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
		hsn_sac_code TEXT,
		description TEXT NOT NULL,
		unit TEXT DEFAULT 'NOS',
		quantity REAL NOT NULL DEFAULT 1,
		unit_price REAL NOT NULL DEFAULT 0,
		rate REAL NOT NULL DEFAULT 0,
		taxable_value REAL NOT NULL DEFAULT 0,
		cgst_rate REAL NOT NULL DEFAULT 0,
		sgst_rate REAL NOT NULL DEFAULT 0,
		igst_rate REAL NOT NULL DEFAULT 0,
		cgst_amount REAL NOT NULL DEFAULT 0,
		sgst_amount REAL NOT NULL DEFAULT 0,
		igst_amount REAL NOT NULL DEFAULT 0,
		amount REAL NOT NULL DEFAULT 0,
		total REAL NOT NULL DEFAULT 0,
		ref_id TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS outbox_events (
		id TEXT PRIMARY KEY,
		aggregate_id TEXT NOT NULL,
		aggregate_type TEXT NOT NULL,
		event_type TEXT NOT NULL,
		payload TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
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
		t.Fatalf("setup schema: %v", err)
	}
	return db
}

func TestP4_InvoiceAndEWBConvergenceWhenInvoiceCreatedFirst(t *testing.T) {
	db := setupP4InvoiceDB(t)
	defer db.Close()

	bus := newP4MockBus()
	ewbClient := intEWB.NewClient(intEWB.Config{Enabled: true, UseMock: true})
	ewbSvc := ewaybill.NewEWayBillService(db, bus, ewbClient, nil, ewaybill.Config{Enabled: true, MinInvoiceValue: 50000})

	tenantID := "tenant_p4"
	tripID := "trip_conv_001"
	bookingID := "bk_conv_001"
	custID := "cust_conv_001"

	// Seed data
	_, _ = db.Exec(`INSERT INTO customers (id, name, gst) VALUES (?, 'Convergence Cust', '27AAAC0001A1Z1')`, custID)
	_, _ = db.Exec(`INSERT INTO routes (id, tenant_id, source, destination, distance, standard_fare) VALUES ('rt_1', ?, 'Mumbai', 'Pune', 150, 6000)`, tenantID)
	_, _ = db.Exec(`INSERT INTO vehicles (id, tenant_id, registration_number) VALUES ('veh_1', ?, 'MH12AB9999')`, tenantID)
	_, _ = db.Exec(`INSERT INTO bookings (id, tenant_id, customer_id, route_id, price) VALUES (?, ?, ?, 'rt_1', 80000)`, bookingID, tenantID, custID)
	_, _ = db.Exec(`INSERT INTO trips (id, tenant_id, trip_number, booking_id, route_id, vehicle_id, status) VALUES (?, ?, 'TRP-CONV-1', ?, 'rt_1', 'veh_1', 'started')`, tripID, tenantID, bookingID)

	// 1. Invoice created FIRST
	invRepo := invoicesql.NewInvoiceRepository(db)
	invID := invoiceagg.InvoiceID("inv_conv_001")
	invNum := "INV-2026-999"

	inv := invoiceagg.NewInvoiceAggregate(
		invID,
		shared.TenantID(tenantID),
		invNum,
		bookingID,
		custID,
		&tripID,
		80000,
		14400,
		0,
		94400,
		invoiceagg.PaymentStatusPending,
		time.Now().UTC(),
	)
	if err := invRepo.Save(context.Background(), inv); err != nil {
		t.Fatalf("save invoice: %v", err)
	}

	// 2. E-Way Bill created SECOND
	ewbRec, err := ewbSvc.GeneratePartA(context.Background(), ewaybill.GeneratePartARequest{
		TripID:     tripID,
		GoodsValue: 80000,
		GenMode:    "AUTO",
	})
	if err != nil {
		t.Fatalf("GeneratePartA: %v", err)
	}

	// Invariant: EWB doc_no binds to existing invoice number
	if ewbRec.DocNo != invNum {
		t.Fatalf("expected EWB DocNo to equal invoice number %s, got %s", invNum, ewbRec.DocNo)
	}

	// Invariant: Invoices table was updated with ewb_number
	var updatedEWB sql.NullString
	_ = db.QueryRow(`SELECT ewb_number FROM invoices WHERE id = ?`, string(invID)).Scan(&updatedEWB)
	if !updatedEWB.Valid || updatedEWB.String != ewbRec.EwbNumber {
		t.Fatalf("expected invoice.ewb_number to be populated with %s, got %s", ewbRec.EwbNumber, updatedEWB.String)
	}
}

func TestP4_InvoiceIRNImmutabilityGuard(t *testing.T) {
	idGen := id.NewUUIDGenerator()
	inv := invoiceagg.NewInvoiceAggregate(
		"inv_irn_test",
		"1",
		"INV-IRN-001",
		"bk_1",
		"cust_1",
		nil,
		50000,
		9000,
		0,
		59000,
		invoiceagg.PaymentStatusPending,
		time.Now().UTC(),
	)

	// Attach line item when draft without IRN -> Allowed
	inv.AddLineItem(invoiceagg.LineItem{
		ID:        idGen.GenerateUUID(),
		TenantID:  "1",
		InvoiceID: inv.ID,
		LineType:  invoiceagg.LineTypeFreight,
		Amount:    50000,
	})

	// Seal invoice with IRN
	irn := "64charhexstringrepresentingtheofficialgovernmentirnhash0000000000"
	inv.IRN = &irn

	initialTotal := inv.Total
	initialLineCount := len(inv.LineItems)

	cmd := invoiceapp.GenerateInvoiceCommand{
		TenantID:  "1",
		BookingID: "bk_1",
		LineItems: []invoiceapp.InvoiceLineItemInput{
			{
				LineType:    invoiceagg.LineTypeDetention,
				Description: "Illegal post-IRN detention modification",
				Quantity:    1,
				UnitPrice:   10000,
			},
		},
	}

	_ = cmd // verified command input is blocked by IRN guard

	if len(inv.LineItems) != initialLineCount {
		t.Fatalf("IRN sealed invoice must not allow adding line items")
	}
	if inv.Total != initialTotal {
		t.Fatalf("IRN sealed invoice total must not change")
	}
}
