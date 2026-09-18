package pnl

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/shared"
)

// Regression for service.go:64-66,81-83,97-111 + :113-128.
// Toll/kharcha query errors are discarded (`_ =`), MarginAvailable depends
// only on FuelCostStatus, and the snapshot is UPDATE'd to trips even when
// cost queries failed — a read (handler.go:13-21 calls Calculate) persists a
// misleading margin with zeroed costs.
func TestCalculate_FailedCostQueryMustNotYieldAvailableMargin(t *testing.T) {
	db, err := sql.Open("sqlite", "file:pnl_defect_costfail?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	// Full schema EXCEPT driver_expenses (forces "no such table" on the
	// toll query at service.go:97-100 and kharcha query at :108-111).
	_, err = db.Exec(`
	CREATE TABLE trips (
		id TEXT PRIMARY KEY, tenant_id TEXT, booking_id TEXT, vehicle_id TEXT, route_id TEXT,
		departure_time DATETIME, arrival_time DATETIME,
		estimated_margin REAL, fuel_consumed_liters REAL, toll_costs REAL, last_pnl_update DATETIME,
		fuel_cost_low REAL, fuel_cost_high REAL, margin_low REAL, margin_high REAL,
		pnl_confidence TEXT, fuel_cost_status TEXT
	);
	CREATE TABLE bookings (id TEXT PRIMARY KEY, price REAL);
	CREATE TABLE vehicles (id TEXT PRIMARY KEY, fuel_type TEXT, current_mileage REAL);
	CREATE TABLE routes (id TEXT PRIMARY KEY);
	CREATE TABLE telemetry_snapshots (trip_id TEXT, odometer REAL);
	CREATE TABLE fuel_prices (tenant_id TEXT, diesel_price REAL, petrol_price REAL, updated_at DATETIME);
	CREATE TABLE maintenance_records (
		id TEXT PRIMARY KEY, vehicle_id TEXT, schedule_id TEXT, service_type TEXT,
		performed_at DATETIME, odometer_km REAL, cost REAL, vendor TEXT, notes TEXT,
		recorded_by TEXT, tenant_id TEXT);
	INSERT INTO trips (id, tenant_id, booking_id, vehicle_id, departure_time, arrival_time)
		VALUES ('trp1', '1', 'bk1', 'veh1', '2026-01-10 08:00:00', '2026-01-12 18:00:00');
	INSERT INTO bookings VALUES ('bk1', 50000);
	INSERT INTO vehicles VALUES ('veh1', 'diesel', 5.0);
	INSERT INTO fuel_prices VALUES ('1', 100.0, 110.0, datetime('now'));
	INSERT INTO telemetry_snapshots VALUES ('trp1', 100), ('trp1', 600);`)
	require.NoError(t, err)

	ctx := shared.ContextWithTenantID(context.Background(), "1")
	p, cerr := NewService(db).Calculate(ctx, "trp1")

	// Desired: failed cost query must mark incomplete/fail — never an
	// available margin with zeroed costs.
	assert.True(t, cerr != nil || !p.MarginAvailable,
		"failed toll/kharcha query must not yield available margin (got err=%v available=%v toll=%v kharcha=%v)",
		cerr, p.MarginAvailable, p.TollCost, p.KharchaApproved)

	// Desired: no misleading snapshot persisted to trips on cost failure.
	var margin sql.NullFloat64
	require.NoError(t, db.QueryRow(`SELECT estimated_margin FROM trips WHERE id = 'trp1'`).Scan(&margin))
	assert.False(t, margin.Valid && margin.Float64 != 0,
		"must not persist misleading snapshot when cost queries failed (estimated_margin=%v)", margin)
}

// Regression for service.go:183-208.
// On "no such column: tenant_id" the fallback retries UNSCOPED
// (sumQuery(false, ...)) — dropping the tenant predicate and aggregating
// other tenants' rows. Compat fallback must never drop tenant scope:
// unknown schema => (0, "unavailable").
func TestFetchMaintenanceCost_NeverDropsTenantPredicate(t *testing.T) {
	db, err := sql.Open("sqlite", "file:pnl_defect_tenant?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	_, err = db.Exec(`
	CREATE TABLE trips (
		id TEXT PRIMARY KEY, tenant_id TEXT, booking_id TEXT, vehicle_id TEXT, route_id TEXT,
		departure_time DATETIME, arrival_time DATETIME,
		estimated_margin REAL, fuel_consumed_liters REAL, toll_costs REAL, last_pnl_update DATETIME,
		fuel_cost_low REAL, fuel_cost_high REAL, margin_low REAL, margin_high REAL,
		pnl_confidence TEXT, fuel_cost_status TEXT
	);
	CREATE TABLE bookings (id TEXT PRIMARY KEY, price REAL);
	CREATE TABLE vehicles (id TEXT PRIMARY KEY, fuel_type TEXT, current_mileage REAL);
	CREATE TABLE routes (id TEXT PRIMARY KEY);
	CREATE TABLE telemetry_snapshots (trip_id TEXT, odometer REAL);
	CREATE TABLE fuel_prices (tenant_id TEXT, diesel_price REAL, petrol_price REAL, updated_at DATETIME);
	CREATE TABLE driver_expenses (
		id TEXT PRIMARY KEY, trip_id TEXT, amount REAL,
		status TEXT, approved INTEGER DEFAULT 0, expense_type TEXT, category TEXT
	);
	CREATE TABLE maintenance_records (
		id TEXT PRIMARY KEY, vehicle_id TEXT, service_type TEXT,
		performed_at DATETIME, cost REAL);
	INSERT INTO trips (id, tenant_id, booking_id, vehicle_id, departure_time, arrival_time)
		VALUES ('trp1', '1', 'bk1', 'veh1', '2026-01-10 08:00:00', '2026-01-12 18:00:00');
	INSERT INTO bookings VALUES ('bk1', 50000);
	INSERT INTO vehicles VALUES ('veh1', 'diesel', 5.0);
	INSERT INTO fuel_prices VALUES ('1', 100.0, 110.0, datetime('now'));
	INSERT INTO telemetry_snapshots VALUES ('trp1', 100), ('trp1', 600);
	INSERT INTO maintenance_records (id, vehicle_id, service_type, performed_at, cost)
		VALUES ('m1', 'veh1', 'oil_change', '2026-01-11 10:00:00', 2000);`)
	require.NoError(t, err)

	ctx := shared.ContextWithTenantID(context.Background(), "1")
	svc := NewService(db)

	cost, status := svc.fetchMaintenanceCost(ctx, "1", "veh1", "trp1")
	assert.Equal(t, "unavailable", status,
		"pre-tenant schema must not fall back to unscoped sum (got cost=%v status=%v)", cost, status)
	assert.InDelta(t, 0, cost, 0.001,
		"tenant predicate must never be dropped (got cost=%v)", cost)

	// End-to-end: Calculate must surface the same unavailability, not an
	// "included" cross-tenant sum in the margin.
	p, cerr := svc.Calculate(ctx, "trp1")
	require.NoError(t, cerr)
	assert.Equal(t, "unavailable", p.MaintenanceCostStatus)
	assert.InDelta(t, 0, p.MaintenanceCost, 0.001)
}
