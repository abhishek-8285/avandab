package service_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"transport-app/internal/service"
)

// openTestDB creates a minimal in-memory SQLite DB with only the pnl_daily
// table (no full migration needed for unit testing the service layer).
func openPNLTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:?_foreign_keys=off")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	schema := `
	CREATE TABLE IF NOT EXISTS pnl_daily (
		id              TEXT PRIMARY KEY,
		tenant_id       TEXT NOT NULL DEFAULT '1',
		snapshot_date   DATE NOT NULL,
		revenue         REAL NOT NULL DEFAULT 0.0,
		expenses        REAL NOT NULL DEFAULT 0.0,
		fuel_costs      REAL NOT NULL DEFAULT 0.0,
		driver_payouts  REAL NOT NULL DEFAULT 0.0,
		maintenance     REAL NOT NULL DEFAULT 0.0,
		toll_costs      REAL NOT NULL DEFAULT 0.0,
		tds_deducted    REAL NOT NULL DEFAULT 0.0,
		net_profit      REAL NOT NULL DEFAULT 0.0,
		trip_count      INTEGER NOT NULL DEFAULT 0,
		vehicle_count   INTEGER NOT NULL DEFAULT 0,
		created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE (tenant_id, snapshot_date)
	);
	-- stubs for aggregate queries
	CREATE TABLE IF NOT EXISTS invoices (
		id TEXT PRIMARY KEY, tenant_id TEXT, total REAL, payment_status TEXT, created_at TEXT
	);
	CREATE TABLE IF NOT EXISTS driver_expenses (
		id TEXT PRIMARY KEY, tenant_id TEXT, amount REAL, category TEXT, created_at TEXT
	);
	CREATE TABLE IF NOT EXISTS driver_settlements (
		id TEXT PRIMARY KEY, trip_id TEXT, net_payout REAL, tds_amount REAL, created_at TEXT
	);
	CREATE TABLE IF NOT EXISTS settlement_lines (
		id TEXT PRIMARY KEY, settlement_id TEXT, trip_id TEXT,
		line_type TEXT, label TEXT, amount REAL, ref_id TEXT, created_at TEXT
	);
	CREATE TABLE IF NOT EXISTS trips (
		id TEXT PRIMARY KEY, tenant_id TEXT, departure_time TEXT, booking_id TEXT
	);
	CREATE TABLE IF NOT EXISTS maintenance_records (
		id TEXT PRIMARY KEY, tenant_id TEXT, cost REAL, performed_at TEXT
	);
	CREATE TABLE IF NOT EXISTS fastag_transactions (
		id TEXT PRIMARY KEY, tenant_id TEXT, amount REAL, txn_timestamp TEXT
	);
	CREATE TABLE IF NOT EXISTS vehicles (
		id TEXT PRIMARY KEY, tenant_id TEXT, status TEXT
	);
	CREATE TABLE IF NOT EXISTS payments (
		id TEXT PRIMARY KEY, tenant_id TEXT, invoice_id TEXT,
		amount REAL, method TEXT, created_at TEXT
	);`

	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	return db
}

func TestPNLService_GenerateDailySnapshot_Zero(t *testing.T) {
	db := openPNLTestDB(t)
	svc := service.NewPNLService(db)
	ctx := context.Background()

	snap, err := svc.GenerateDailySnapshot(ctx, "1", time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if snap.TenantID != "1" {
		t.Errorf("tenant_id = %q, want '1'", snap.TenantID)
	}
	if snap.SnapshotDate != "2025-01-15" {
		t.Errorf("snapshot_date = %q, want '2025-01-15'", snap.SnapshotDate)
	}
	// No data → all zeros
	if snap.Revenue != 0 || snap.NetProfit != 0 {
		t.Errorf("expected zeros, got revenue=%f net=%f", snap.Revenue, snap.NetProfit)
	}
	if snap.ID == "" {
		t.Error("ID must not be empty")
	}
}

func TestPNLService_GenerateDailySnapshot_WithData(t *testing.T) {
	db := openPNLTestDB(t)
	svc := service.NewPNLService(db)
	ctx := context.Background()
	date := "2025-03-10"

	// Seed: one paid invoice for $1000
	db.Exec(`INSERT INTO invoices VALUES ('inv1','1',1000.0,'paid',?)`, date+" 10:00:00")
	// Seed: fuel expense $200
	db.Exec(`INSERT INTO driver_expenses VALUES ('de1','1',200.0,'fuel',?)`, date+" 08:00:00")
	// Seed: trip + settlement payout $300
	db.Exec(`INSERT INTO trips VALUES ('t1','1',?,'bk1')`, date+"T09:00:00")
	db.Exec(`INSERT INTO driver_settlements VALUES ('ds1','t1',300.0,30.0,?)`, date+" 18:00:00")
	// Seed: maintenance $50
	db.Exec(`INSERT INTO maintenance_records VALUES ('mr1','1',50.0,?)`, date+"T10:00:00")
	// Seed: toll $20
	db.Exec(`INSERT INTO fastag_transactions VALUES ('ft1','1',20.0,?)`, date+" 12:00:00")
	// Seed: 2 active vehicles
	db.Exec(`INSERT INTO vehicles VALUES ('v1','1','active'), ('v2','1','active')`)

	snap, err := svc.GenerateDailySnapshot(ctx, "1", time.Date(2025, 3, 10, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if snap.Revenue != 1000 {
		t.Errorf("revenue = %f, want 1000", snap.Revenue)
	}
	if snap.FuelCosts != 200 {
		t.Errorf("fuel_costs = %f, want 200", snap.FuelCosts)
	}
	if snap.DriverPayouts != 300 {
		t.Errorf("driver_payouts = %f, want 300", snap.DriverPayouts)
	}
	if snap.Maintenance != 50 {
		t.Errorf("maintenance = %f, want 50", snap.Maintenance)
	}
	if snap.TollCosts != 20 {
		t.Errorf("toll_costs = %f, want 20", snap.TollCosts)
	}
	if snap.TdsDeducted != 30 {
		t.Errorf("tds_deducted = %f, want 30", snap.TdsDeducted)
	}
	wantExpenses := 200.0 + 300.0 + 50.0 + 20.0 // 570
	if snap.Expenses != wantExpenses {
		t.Errorf("expenses = %f, want %f", snap.Expenses, wantExpenses)
	}
	wantNet := 1000.0 - 570.0 // 430
	if snap.NetProfit != wantNet {
		t.Errorf("net_profit = %f, want %f", snap.NetProfit, wantNet)
	}
	if snap.TripCount != 1 {
		t.Errorf("trip_count = %d, want 1", snap.TripCount)
	}
	if snap.VehicleCount != 2 {
		t.Errorf("vehicle_count = %d, want 2", snap.VehicleCount)
	}
}

func TestPNLService_GenerateDailySnapshot_Idempotent(t *testing.T) {
	db := openPNLTestDB(t)
	svc := service.NewPNLService(db)
	ctx := context.Background()
	d := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)

	snap1, err := svc.GenerateDailySnapshot(ctx, "1", d)
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}

	// Second call for same date must UPDATE not INSERT (no UNIQUE violation)
	snap2, err := svc.GenerateDailySnapshot(ctx, "1", d)
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}

	// Both represent same date
	if snap1.SnapshotDate != snap2.SnapshotDate {
		t.Errorf("dates differ: %s vs %s", snap1.SnapshotDate, snap2.SnapshotDate)
	}

	// Verify only one row in DB
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM pnl_daily WHERE tenant_id='1' AND snapshot_date='2025-06-01'`).Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 row, got %d", count)
	}
}

func TestPNLService_GetPNLRange(t *testing.T) {
	db := openPNLTestDB(t)
	svc := service.NewPNLService(db)
	ctx := context.Background()

	dates := []time.Time{
		time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 1, 3, 0, 0, 0, 0, time.UTC),
	}
	for _, d := range dates {
		if _, err := svc.GenerateDailySnapshot(ctx, "1", d); err != nil {
			t.Fatalf("generate %s: %v", d.Format("2006-01-02"), err)
		}
	}

	// Range covering all 3
	snaps, err := svc.GetPNLRange(ctx, "1",
		time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 1, 3, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("get range: %v", err)
	}
	if len(snaps) != 3 {
		t.Errorf("got %d snapshots, want 3", len(snaps))
	}

	// Narrow range — only day 2
	snaps2, err := svc.GetPNLRange(ctx, "1",
		time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("narrow range: %v", err)
	}
	if len(snaps2) != 1 {
		t.Errorf("got %d snapshots, want 1", len(snaps2))
	}

	// Empty range (wrong tenant)
	snaps3, err := svc.GetPNLRange(ctx, "other",
		time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 1, 3, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("other tenant: %v", err)
	}
	if len(snaps3) != 0 {
		t.Errorf("expected 0 for other tenant, got %d", len(snaps3))
	}
}

func TestGetActiveTenantIDs_FallbackToDefault(t *testing.T) {
	db := openPNLTestDB(t)
	ctx := context.Background()

	// Empty trips table → should return default tenant
	ids, err := service.GetActiveTenantIDs(ctx, db)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 1 || ids[0] != "1" {
		t.Errorf("expected ['1'], got %v", ids)
	}
}

func TestGetActiveTenantIDs_MultiTenant(t *testing.T) {
	db := openPNLTestDB(t)
	ctx := context.Background()

	db.Exec(`INSERT INTO trips VALUES ('t1','tenant-a','2025-01-01','b1')`)
	db.Exec(`INSERT INTO trips VALUES ('t2','tenant-b','2025-01-02','b2')`)
	db.Exec(`INSERT INTO trips VALUES ('t3','tenant-a','2025-01-03','b3')`)

	ids, err := service.GetActiveTenantIDs(ctx, db)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 2 {
		t.Errorf("expected 2 distinct tenants, got %d: %v", len(ids), ids)
	}
}

func TestPNLService_GetMoneyStrip_MatchesSnapshot(t *testing.T) {
	db := openPNLTestDB(t)
	svc := service.NewPNLService(db)
	ctx := context.Background()
	when := time.Date(2025, 3, 10, 12, 0, 0, 0, time.UTC)
	date := "2025-03-10"

	// Same fixture shape as TestPNLService_GenerateDailySnapshot_WithData.
	db.Exec(`INSERT INTO invoices VALUES ('inv1','1',1000.0,'paid',?)`, date+" 10:00:00")
	db.Exec(`INSERT INTO driver_expenses VALUES ('de1','1',200.0,'fuel',?)`, date+" 08:00:00")
	db.Exec(`INSERT INTO trips VALUES ('t1','1',?,'b1')`, date+" 06:00:00")
	db.Exec(`INSERT INTO driver_settlements VALUES ('ds1','t1',300.0,30.0,?)`, date+" 18:00:00")
	db.Exec(`INSERT INTO maintenance_records VALUES ('mr1','1',50.0,?)`, date+" 09:00:00")
	db.Exec(`INSERT INTO fastag_transactions VALUES ('ft1','1',40.0,?)`, date+" 07:00:00")

	snap, err := svc.GenerateDailySnapshot(ctx, "1", when)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	strip, err := svc.GetMoneyStrip(ctx, "1", when)
	if err != nil {
		t.Fatalf("money strip: %v", err)
	}

	if strip.Date != snap.SnapshotDate {
		t.Errorf("date = %q, want %q", strip.Date, snap.SnapshotDate)
	}
	// Exit gate (Spec 22 §10 Step 2): strip must equal report totals.
	if strip.Revenue != snap.Revenue {
		t.Errorf("revenue = %f, want %f (report total)", strip.Revenue, snap.Revenue)
	}
	if strip.Spent != snap.Expenses {
		t.Errorf("spent = %f, want %f (report total)", strip.Spent, snap.Expenses)
	}
}

func TestPNLService_GetMoneyStrip_Receivables(t *testing.T) {
	db := openPNLTestDB(t)
	svc := service.NewPNLService(db)
	ctx := context.Background()
	when := time.Date(2025, 3, 10, 12, 0, 0, 0, time.UTC)

	// inv_a: 1000 total, 400 paid → 600 outstanding.
	db.Exec(`INSERT INTO invoices VALUES ('inv_a','1',1000.0,'partially_paid','2025-03-01')`)
	db.Exec(`INSERT INTO payments VALUES ('pay_a','1','inv_a',400.0,'upi','2025-03-02')`)
	// inv_b: 500 total, nothing paid → 500 outstanding.
	db.Exec(`INSERT INTO invoices VALUES ('inv_b','1',500.0,'pending','2025-03-03')`)
	// inv_c: overpaid 350 against 300 → floors at 0, never negative.
	db.Exec(`INSERT INTO invoices VALUES ('inv_c','1',300.0,'paid','2025-03-04')`)
	db.Exec(`INSERT INTO payments VALUES ('pay_c','1','inv_c',350.0,'cash','2025-03-05')`)
	// Other tenant's invoice must not leak in.
	db.Exec(`INSERT INTO invoices VALUES ('inv_x','2',9999.0,'pending','2025-03-05')`)

	strip, err := svc.GetMoneyStrip(ctx, "1", when)
	if err != nil {
		t.Fatalf("money strip: %v", err)
	}
	if strip.Receivables != 1100.0 {
		t.Errorf("receivables = %f, want 1100.0 (600+500+0, tenant '1' only)", strip.Receivables)
	}
}
