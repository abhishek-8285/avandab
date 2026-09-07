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

func newPnLTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:pnl_svc_test?mode=memory&cache=shared")
	require.NoError(t, err)
	schema := `
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
		id TEXT PRIMARY KEY, vehicle_id TEXT, schedule_id TEXT, service_type TEXT,
		performed_at DATETIME, odometer_km REAL, cost REAL, vendor TEXT, notes TEXT,
		recorded_by TEXT, tenant_id TEXT);`
	_, err = db.Exec(schema)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

func seedTrip(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, q := range []string{
		`INSERT INTO trips (id, tenant_id, booking_id, vehicle_id, departure_time, arrival_time) VALUES ('trp1', '1', 'bk1', 'veh1', '2026-01-10 08:00:00', '2026-01-12 18:00:00')`,
		`INSERT INTO bookings VALUES ('bk1', 50000)`,
		`INSERT INTO vehicles VALUES ('veh1', 'diesel', 5.0)`,
		`INSERT INTO fuel_prices VALUES ('1', 100.0, 110.0, datetime('now'))`,
	} {
		_, err := db.Exec(q)
		require.NoError(t, err)
	}
}

func insertExpense(t *testing.T, db *sql.DB, id, etype, category string, amount float64) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO driver_expenses (id, trip_id, amount, status, approved, expense_type, category)
		VALUES (?, 'trp1', ?, 'approved', 1, ?, ?)`, id, amount, etype, category)
	require.NoError(t, err)
}

// With a telemetry-based fuel estimate, approved FUEL claims describe the
// same spend as FuelCost — they must be excluded from KharchaApproved so
// the margin does not subtract them twice.
func TestCalculate_FuelKharchaNotDoubleCountedAgainstTelemetryEstimate(t *testing.T) {
	db := newPnLTestDB(t)
	seedTrip(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	// Telemetry: 500 km at 5 km/l → 100 L × ₹100 = ₹10,000 estimated fuel.
	_, err := db.Exec(`INSERT INTO telemetry_snapshots VALUES ('trp1', 100), ('trp1', 600)`)
	require.NoError(t, err)

	insertExpense(t, db, "de-fuel", "", "fuel", 3000)
	insertExpense(t, db, "de-park", "parking", "parking", 500)

	p, err := NewService(db).Calculate(ctx, "trp1")
	require.NoError(t, err)

	assert.Equal(t, "estimated", p.FuelCostStatus)
	assert.InDelta(t, 10000, p.FuelCost, 0.001)
	assert.InDelta(t, 500, p.KharchaApproved, 0.001,
		"fuel claim must not stack on telemetry estimate")
	assert.InDelta(t, 39500, p.EstimatedMargin, 0.001,
		"margin = 50000 fare − 10000 fuel − 500 kharcha (fuel claim excluded, no tolls seeded)")
}

// Without telemetry there is no estimate — the approved fuel claim is the
// only fuel signal and must still surface in KharchaApproved.
func TestCalculate_WithoutTelemetry_FuelClaimsAreTheOnlySignal(t *testing.T) {
	db := newPnLTestDB(t)
	seedTrip(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	insertExpense(t, db, "de-fuel2", "", "fuel", 3000)

	p, err := NewService(db).Calculate(ctx, "trp1")
	require.NoError(t, err)

	assert.Equal(t, "pending_verification", p.FuelCostStatus)
	assert.False(t, p.MarginAvailable)
	assert.InDelta(t, 3000, p.KharchaApproved, 0.001,
		"without an estimate, fuel claim must still surface")
}

// Maintenance records for the trip vehicle inside the trip window must
// reduce the margin: fare − fuel − toll − kharcha − maintenance.
func TestCalculate_MaintenanceCostReducesMargin(t *testing.T) {
	db := newPnLTestDB(t)
	seedTrip(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	// Telemetry: 500 km at 5 km/l → 100 L × ₹100 = ₹10,000 estimated fuel.
	_, err := db.Exec(`INSERT INTO telemetry_snapshots VALUES ('trp1', 100), ('trp1', 600)`)
	require.NoError(t, err)

	insertExpense(t, db, "de-park", "parking", "parking", 500)
	// In-window record: counts.
	_, err = db.Exec(`INSERT INTO maintenance_records
		(id, vehicle_id, service_type, performed_at, cost, tenant_id)
		VALUES ('m1', 'veh1', 'oil_change', '2026-01-11 10:00:00', 2000, '1')`)
	require.NoError(t, err)
	// Out-of-window record: ignored.
	_, err = db.Exec(`INSERT INTO maintenance_records
		(id, vehicle_id, service_type, performed_at, cost, tenant_id)
		VALUES ('m2', 'veh1', 'brake', '2026-02-01 10:00:00', 9000, '1')`)
	require.NoError(t, err)
	// Foreign-tenant record inside the window: ignored (tenant-scoped).
	_, err = db.Exec(`INSERT INTO maintenance_records
		(id, vehicle_id, service_type, performed_at, cost, tenant_id)
		VALUES ('m3', 'veh1', 'tyre', '2026-01-11 12:00:00', 7000, '2')`)
	require.NoError(t, err)

	p, err := NewService(db).Calculate(ctx, "trp1")
	require.NoError(t, err)

	assert.Equal(t, "included", p.MaintenanceCostStatus)
	assert.InDelta(t, 2000, p.MaintenanceCost, 0.001)
	assert.InDelta(t, 37500, p.EstimatedMargin, 0.001,
		"margin = 50000 fare − 10000 fuel − 0 toll − 500 kharcha − 2000 maintenance")
}

// Without a maintenance_records table the PnL must still compute with
// zero maintenance cost flagged unavailable (backward compat).
func TestCalculate_MaintenanceTableMissingIsUnavailable(t *testing.T) {
	db, err := sql.Open("sqlite", "file:pnl_svc_nomaint?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	_, err = db.Exec(`
	CREATE TABLE trips (
		id TEXT PRIMARY KEY, tenant_id TEXT, booking_id TEXT, vehicle_id TEXT, route_id TEXT,
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
	INSERT INTO trips (id, tenant_id, booking_id, vehicle_id) VALUES ('trp1', '1', 'bk1', 'veh1');
	INSERT INTO bookings VALUES ('bk1', 50000);
	INSERT INTO vehicles VALUES ('veh1', 'diesel', 5.0);
	INSERT INTO fuel_prices VALUES ('1', 100.0, 110.0, datetime('now'));
	INSERT INTO telemetry_snapshots VALUES ('trp1', 100), ('trp1', 600);`)
	require.NoError(t, err)

	ctx := shared.ContextWithTenantID(context.Background(), "1")
	p, err := NewService(db).Calculate(ctx, "trp1")
	require.NoError(t, err)

	assert.Equal(t, "unavailable", p.MaintenanceCostStatus)
	assert.InDelta(t, 0, p.MaintenanceCost, 0.001)
	assert.InDelta(t, 40000, p.EstimatedMargin, 0.001,
		"margin = 50000 fare − 10000 fuel, no maintenance table")
}
