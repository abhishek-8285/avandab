package ewaybill_test

// Ratchet: the demo stub must never fabricate EWB numbers into the billing
// ledger (eway_bills / trips.eway_bill_ref / invoices.ewb_number) via trip
// lifecycle events or the expiry monitor. Manual GeneratePartA stays allowed.

import (
	"context"
	"database/sql"
	"log/slog"
	"testing"
	"time"

	"transport-app/internal/events"
	"transport-app/internal/ewaybill"
	intEWB "transport-app/internal/integration/ewaybill"
)

// fakeRealProvider is a non-stub Client: it stands in for the live NIC client
// when tests need auto-generation without the mock-provider guard.
type fakeRealProvider struct{}

func (fakeRealProvider) GeneratePartA(_ context.Context, req intEWB.GenerateRequest) (intEWB.EWayBill, error) {
	now := time.Now()
	return intEWB.EWayBill{
		EwbNumber:   "EWB-TEST-" + req.DocumentNumber,
		Status:      "PART_A_GENERATED",
		GeneratedAt: now,
		ValidUpto:   now.Add(24 * time.Hour),
	}, nil
}

func (f fakeRealProvider) Generate(ctx context.Context, req intEWB.GenerateRequest) (intEWB.EWayBill, error) {
	return f.GeneratePartA(ctx, req)
}

func (fakeRealProvider) Get(_ context.Context, ewbNumber string) (intEWB.EWayBill, error) {
	return intEWB.EWayBill{EwbNumber: ewbNumber, Status: "ACTIVE"}, nil
}

func (fakeRealProvider) Cancel(_ context.Context, ewbNumber, reason string) (intEWB.Cancellation, error) {
	return intEWB.Cancellation{EwbNumber: ewbNumber, Reason: reason, Status: "CANCELLED"}, nil
}

func (fakeRealProvider) AttachPartB(_ context.Context, ewbNumber, _, _ string) (intEWB.EWayBill, error) {
	return intEWB.EWayBill{EwbNumber: ewbNumber, Status: "ACTIVE"}, nil
}

func (fakeRealProvider) Extend(_ context.Context, ewbNumber string, _ intEWB.ExtendRequest) (intEWB.EWayBill, error) {
	return intEWB.EWayBill{EwbNumber: ewbNumber, Status: "EXTENDED", ValidUpto: time.Now().Add(24 * time.Hour)}, nil
}

func (f fakeRealProvider) GetByNumber(ctx context.Context, ewbNumber string) (intEWB.EWayBill, error) {
	return f.Get(ctx, ewbNumber)
}

func (fakeRealProvider) GetByTrip(_ context.Context, tripID string) (intEWB.EWayBill, error) {
	return intEWB.EWayBill{EwbNumber: "EWB-TEST-" + tripID, Status: "ACTIVE"}, nil
}

func seedAutoGenerateTrip(t *testing.T, db *sql.DB) {
	t.Helper()
	_, _ = db.Exec(`INSERT INTO customers (id, name, gst) VALUES ('cust_mock', 'Cust Mock', '27AAAC0003C1Z3')`)
	_, _ = db.Exec(`INSERT INTO routes (id, tenant_id, source, destination, distance, standard_fare) VALUES ('rt_mock', 'tenant_mock', 'Mumbai', 'Pune', 150, 5000)`)
	_, _ = db.Exec(`INSERT INTO bookings (id, tenant_id, customer_id, route_id, price) VALUES ('bk_mock', 'tenant_mock', 'cust_mock', 'rt_mock', 90000)`)
	_, _ = db.Exec(`INSERT INTO trips (id, tenant_id, trip_number, booking_id, route_id, status) VALUES ('trip_mock', 'tenant_mock', 'TRP-MOCK-1', 'bk_mock', 'rt_mock', 'started')`)
}

func confirmTrip(bus *mockRecordedBus) {
	bus.Publish(context.Background(), events.Event{
		Type:    "TripConfirmedEvent",
		Payload: map[string]interface{}{"trip_id": "trip_mock", "tenant_id": "tenant_mock"},
	})
}

func assertNoFabricatedEWB(t *testing.T, db *sql.DB) {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM eway_bills WHERE trip_id = 'trip_mock'`).Scan(&count); err != nil {
		t.Fatalf("count eway_bills: %v", err)
	}
	if count != 0 {
		t.Fatalf("mock provider wrote %d fabricated eway_bills row(s), want 0", count)
	}
	var ref sql.NullString
	if err := db.QueryRow(`SELECT eway_bill_ref FROM trips WHERE id = 'trip_mock'`).Scan(&ref); err != nil {
		t.Fatalf("read trips.eway_bill_ref: %v", err)
	}
	if ref.Valid && ref.String != "" {
		t.Fatalf("mock provider stamped trips.eway_bill_ref = %q, want NULL", ref.String)
	}
}

func TestMockProvider_DoesNotAutoGenerate(t *testing.T) {
	db := setupP4TestDB(t)
	defer db.Close()
	seedAutoGenerateTrip(t, db)

	bus := newMockRecordedBus()
	client := intEWB.NewClient(intEWB.Config{Enabled: true, UseMock: true})
	svc := ewaybill.NewEWayBillService(db, bus, client, slog.Default(), ewaybill.Config{Enabled: true, MinInvoiceValue: 50000})
	if !svc.IsMockProvider() {
		t.Fatal("stub client must be reported as mock provider")
	}
	svc.SubscribeTripEvents(bus)

	confirmTrip(bus)

	assertNoFabricatedEWB(t, db)
}

func TestMonitorRun_ReturnsImmediatelyForMockProvider(t *testing.T) {
	svc := ewaybill.NewEWayBillService(nil, nil, nil, slog.Default(), ewaybill.Config{})
	monitor := ewaybill.NewMonitor(svc, ewaybill.Config{Interval: time.Hour})

	done := make(chan struct{})
	go func() {
		monitor.Run(context.Background())
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Monitor.Run must return immediately when the provider is mock")
	}
}

func TestNonMockProvider_AutoGenerates(t *testing.T) {
	db := setupP4TestDB(t)
	defer db.Close()
	seedAutoGenerateTrip(t, db)

	bus := newMockRecordedBus()
	svc := ewaybill.NewEWayBillService(db, bus, fakeRealProvider{}, slog.Default(), ewaybill.Config{Enabled: true, MinInvoiceValue: 50000})
	if svc.IsMockProvider() {
		t.Fatal("fake live client must not be reported as mock provider")
	}
	svc.SubscribeTripEvents(bus)

	confirmTrip(bus)

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM eway_bills WHERE trip_id = 'trip_mock' AND gen_mode = 'AUTO'`).Scan(&count); err != nil {
		t.Fatalf("count eway_bills: %v", err)
	}
	if count != 1 {
		t.Fatalf("non-mock provider must auto-generate exactly 1 eway_bills row, got %d", count)
	}
	var ref sql.NullString
	if err := db.QueryRow(`SELECT eway_bill_ref FROM trips WHERE id = 'trip_mock'`).Scan(&ref); err != nil {
		t.Fatalf("read trips.eway_bill_ref: %v", err)
	}
	if !ref.Valid || ref.String == "" {
		t.Fatal("non-mock provider must stamp trips.eway_bill_ref")
	}
}
