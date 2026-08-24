package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPilotKPIs_Computation(t *testing.T) {
	db := newVerifyTestDB(t)
	svc := NewKPIService(db)
	ctx := context.Background()

	// Trips: tenant '1' two trips; one settled fast, one disputed.
	mustRadar(t, db, `INSERT OR IGNORE INTO routes (id, source, destination, distance, estimated_hours, standard_fare)
		VALUES ('rt-kpi', 'A', 'B', 100, 2, 5000)`)
	mustRadar(t, db, `INSERT INTO trips (id, trip_number, route_id, departure_time, tenant_id, status)
		VALUES ('tr-k1','TR-K1','rt-kpi', datetime('now','-3 days'), 'tn-kpi', 'completed'),
		       ('tr-k2','TR-K2','rt-kpi', datetime('now','-2 days'), 'tn-kpi', 'completed')`)

	// Settlements: k1 paid in 5 minutes (<10 target), k2 disputed, k3
	// belongs to another tenant and must not count.
	mustRadar(t, db, `INSERT INTO driver_settlements (id, trip_id, driver_id, status, created_at, paid_at)
		VALUES ('st-1','tr-k1','d1','paid',     datetime('now','-3 days'), datetime('now','-3 days','+5 minutes')),
		       ('st-2','tr-k2','d1','disputed', datetime('now','-2 days'), NULL),
		       ('st-3','tr-k3','dx','paid',     datetime('now','-1 days'), datetime('now','-1 days'))`)

	// Expenses: 9 app-submitted (idempotency key) + 1 manual → 90%.
	for i := 0; i < 9; i++ {
		mustRadar(t, db, `INSERT INTO driver_expenses (id, expense_type, category, amount,
			status, idempotency_key, tenant_id, driver_id)
			VALUES ('kx-' || ?, 'fuel', 'fuel', 100, 'pending', 'idem-' || ?, 'tn-kpi', 'dk-a')`, i, i)
	}
	mustRadar(t, db, `INSERT INTO driver_expenses (id, expense_type, category, amount,
		status, tenant_id) VALUES ('kx-manual', 'food', 'food', 50, 'pending', 'tn-kpi')`)

	// Drivers for WAU denominator + activity via expenses above.
	mustRadar(t, db, `INSERT INTO drivers (id, driver_id, first_name, last_name, phone,
		license_number, license_expiry, status, tenant_id)
		VALUES ('d1','DK1','A','B','+91-9000000001','DL-1','2030-01-01','available','tn-kpi'),
		       ('d2','DK2','C','D','+91-9000000002','DL-2','2030-01-01','available','tn-kpi')`)
	// d2 inactive this week (no claims/trips), d1 active via expenses.

	kpis, err := svc.PilotKPIs(ctx, "tn-kpi", 14)
	require.NoError(t, err)

	require.NotNil(t, kpis.SettlementCycleMin)
	assert.Less(t, *kpis.SettlementCycleMin, 10.0, "5-minute paid settlement must beat the <10min target")
	require.NotNil(t, kpis.KharchaAppSubmitted)
	assert.InDelta(t, 90.0, *kpis.KharchaAppSubmitted, 0.01)
	require.NotNil(t, kpis.DisputedSettlements)
	assert.InDelta(t, 50.0, *kpis.DisputedSettlements, 0.01, "1 disputed of 2 counted settlements")
	require.NotNil(t, kpis.DriverWAU)
	assert.InDelta(t, 100.0, *kpis.DriverWAU, 0.01,
		"dk-a active by claim, d1 active via settlement — both active")

	// Usage counters: zero before any console_open events.
	assert.Equal(t, 0, kpis.ConsoleOpens)
}

func TestPilotKPIs_TenantIsolationAndEmptyWindow(t *testing.T) {
	db := newVerifyTestDB(t)
	svc := NewKPIService(db)

	// No data at all → nil metrics, never fake zeros.
	kpis, err := svc.PilotKPIs(context.Background(), "ghost", 14)
	require.NoError(t, err)
	assert.Nil(t, kpis.SettlementCycleMin)
	assert.Nil(t, kpis.KharchaAppSubmitted)
	assert.Nil(t, kpis.EwbExpiryCaught)
	assert.Equal(t, 14, kpis.WindowDays)
}

func TestRecordConsoleUsage_WritesRow(t *testing.T) {
	db := newVerifyTestDB(t)
	svc := NewKPIService(db)
	svc.RecordConsoleUsage(context.Background(), "1", "user-9", "console_open")

	var n int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM experiment_events
		WHERE experiment='command_center' AND event='console_open' AND user_id='user-9'`).Scan(&n))
	assert.Equal(t, 1, n)

	// Empty user/event are dropped silently (best-effort analytics).
	svc.RecordConsoleUsage(context.Background(), "1", "", "console_open")
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM experiment_events`).Scan(&n))
	assert.Equal(t, 1, n)
}

func TestStorageStats_CountsAndPragmas(t *testing.T) {
	db := newVerifyTestDB(t)
	svc := NewKPIService(db)
	seedExpense(t, db, "st-x1", nil, nil, 100, false)

	stats, err := svc.StorageStats(context.Background())
	require.NoError(t, err)

	assert.Greater(t, stats.TotalDBBytes, int64(0), "page_count×page_size must be positive")
	var seen bool
	for _, tbl := range stats.Tables {
		if tbl.Table == "driver_expenses" {
			seen = true
			assert.GreaterOrEqual(t, tbl.Rows, int64(1))
		}
	}
	assert.True(t, seen, "watched tables must include driver_expenses")
}

func TestPilotKPIs_IncludesStorage(t *testing.T) {
	db := newVerifyTestDB(t)
	svc := NewKPIService(db)
	kpis, err := svc.PilotKPIs(context.Background(), "ghost", 14)
	require.NoError(t, err)
	require.NotNil(t, kpis.Storage)
	assert.Greater(t, kpis.Storage.TotalDBBytes, int64(0))
}
