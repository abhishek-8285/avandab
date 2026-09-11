package ewaybill_test

// A9 staging proof: the delivery path runs against the REAL migration chain
// (not the hand-written P4 schema), so 00127/00128 CHECK values are exercised
// end to end — TripDeliveredEvent must flip the bill to 'delivered' and append
// the DELIVERED audit event on migrated schema.
import (
	"context"
	"database/sql"
	"io/fs"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"

	dbmigr "transport-app/db"
	"transport-app/internal/events"
	"transport-app/internal/ewaybill"
	intEWB "transport-app/internal/integration/ewaybill"
)

func TestA9_TripDeliveryOnMigratedChain(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "a9.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	migFS, err := fs.Sub(dbmigr.Migrations, "migrations")
	if err != nil {
		t.Fatalf("embed fs: %v", err)
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, migFS)
	if err != nil {
		t.Fatalf("goose provider: %v", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("migrate to head: %v", err)
	}
	var version int
	if err := db.QueryRow(`SELECT max(version_id) FROM goose_db_version`).Scan(&version); err != nil {
		t.Fatalf("goose version: %v", err)
	}
	if version < 128 {
		t.Fatalf("chain at %d, want >= 128 (00127/00128 delivered lifecycle)", version)
	}

	bus := newMockRecordedBus()
	client := intEWB.NewClient(intEWB.Config{Enabled: true, UseMock: true})
	svc := ewaybill.NewEWayBillService(db, bus, client, slog.Default(), ewaybill.Config{Enabled: true})
	svc.SubscribeTripEvents(bus)

	const tripID, ewbNumber = "TRIP-A9-STAGING-001", "888800009999"
	if _, err := db.Exec(`INSERT INTO eway_bills (id, trip_id, ewb_number, status, generation_date, valid_until) VALUES ('ewb_a9_1', ?, ?, 'active', datetime('now'), datetime('now', '+1 day'))`, tripID, ewbNumber); err != nil {
		t.Fatalf("seed ewb: %v", err)
	}

	bus.Publish(ctx, events.Event{Type: "TripDeliveredEvent", Payload: map[string]interface{}{"trip_id": tripID, "tenant_id": "1"}})

	var status string
	if err := db.QueryRow(`SELECT status FROM eway_bills WHERE ewb_number = ?`, ewbNumber).Scan(&status); err != nil {
		t.Fatalf("bill status: %v", err)
	}
	if status != "delivered" {
		t.Fatalf("status = %q, want 'delivered' (00128 CHECK)", status)
	}
	var eventType string
	if err := db.QueryRow(`SELECT event_type FROM eway_bill_events WHERE ewb_number = ?`, ewbNumber).Scan(&eventType); err != nil {
		t.Fatalf("audit event: %v", err)
	}
	if eventType != "DELIVERED" {
		t.Fatalf("event_type = %q, want 'DELIVERED' (00127 CHECK)", eventType)
	}
}
