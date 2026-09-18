package service_test

import (
	"context"
	"testing"
	"time"

	"transport-app/internal/service"
)

// Red test: failed daily aggregate must not persist a zeroed snapshot.
// Mirrors internal/pnl contract (toll/kharcha/telemetry/fuel errors return,
// maintenance degrades to unavailable).
func TestPNLService_GenerateDailySnapshot_AggregateFailureMustNotPersistZeroed(t *testing.T) {
	db := openPNLTestDB(t)
	svc := service.NewPNLService(db)
	ctx := context.Background()
	d := time.Date(2025, 3, 10, 0, 0, 0, 0, time.UTC)

	// Force toll aggregate to fail ("no such table").
	if _, err := db.Exec(`DROP TABLE fastag_transactions`); err != nil {
		t.Fatalf("drop fastag_transactions: %v", err)
	}

	_, err := svc.GenerateDailySnapshot(ctx, "1", d)
	if err == nil {
		t.Fatalf("expected error on failed toll aggregate, got nil (would persist zeroed snapshot)")
	}

	var count int
	if qerr := db.QueryRow(`SELECT COUNT(*) FROM pnl_daily WHERE tenant_id='1' AND snapshot_date='2025-03-10'`).Scan(&count); qerr != nil {
		t.Fatalf("count snapshots: %v", qerr)
	}
	if count != 0 {
		t.Fatalf("failed aggregate persisted %d zeroed snapshot row(s), want 0", count)
	}
}

// Maintenance failure degrades (unavailable → 0) without failing the snapshot,
// mirroring internal/pnl fetchMaintenanceCost (0, "unavailable").
func TestPNLService_GenerateDailySnapshot_MaintenanceFailureDegrades(t *testing.T) {
	db := openPNLTestDB(t)
	svc := service.NewPNLService(db)
	ctx := context.Background()
	d := time.Date(2025, 3, 10, 0, 0, 0, 0, time.UTC)

	if _, err := db.Exec(`DROP TABLE maintenance_records`); err != nil {
		t.Fatalf("drop maintenance_records: %v", err)
	}

	snap, err := svc.GenerateDailySnapshot(ctx, "1", d)
	if err != nil {
		t.Fatalf("maintenance failure must degrade, got error: %v", err)
	}
	if snap.Maintenance != 0 {
		t.Fatalf("maintenance = %f, want 0 (degraded unavailable)", snap.Maintenance)
	}
	var count int
	if qerr := db.QueryRow(`SELECT COUNT(*) FROM pnl_daily WHERE tenant_id='1' AND snapshot_date='2025-03-10'`).Scan(&count); qerr != nil {
		t.Fatalf("count snapshots: %v", qerr)
	}
	if count != 1 {
		t.Fatalf("expected degraded snapshot persisted, got %d rows", count)
	}
}
