package db

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestMigration00143MaintenancePlans(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "mig_00143.db")
	database, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer database.Close()

	ctx := context.Background()
	migFS, err := fs.Sub(Migrations, "migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectSQLite3, database, migFS)
	require.NoError(t, err)

	_, err = provider.UpTo(ctx, 143)
	require.NoError(t, err)

	assertForeignKeyCheckClean(t, database, "after 00143 up")

	// Seed vehicle and measuring point
	_, err = database.Exec(`
		INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, status, tenant_id, fleet_class)
		VALUES ('veh-plan-1', 'KA01PL1001', 'V-PL-01', 'truck', 10000, 'available', '1', 'CV')
	`)
	require.NoError(t, err)

	_, err = database.Exec(`
		INSERT INTO vehicle_measuring_points (id, tenant_id, vehicle_id, kind, meas_position, unit, annual_estimate, description)
		VALUES ('mp-plan-1', '1', 'veh-plan-1', 'ODO', 'DISTANCE', 'KM', 50000.0, 'Truck Odometer')
	`)
	require.NoError(t, err)

	// Valid maintenance plan insert
	_, err = database.Exec(`
		INSERT INTO maintenance_plans (id, tenant_id, plan_number, vehicle_id, measuring_point_id, service_type, cycle_interval_km, cycle_interval_days, call_horizon_percent, next_due_km, status)
		VALUES ('mp-1', '1', 'MP-1001', 'veh-plan-1', 'mp-plan-1', 'oil_change', 10000.0, 180, 90.0, 10000.0, 'active')
	`)
	require.NoError(t, err)

	// Verify work_orders now has plan_id and due_km
	_, err = database.Exec(`
		INSERT INTO work_orders (id, tenant_id, vehicle_id, plan_id, due_km, title, status)
		VALUES ('wo-plan-1', '1', 'veh-plan-1', 'mp-1', 10000.0, 'IP41: Oil Change', 'open')
	`)
	require.NoError(t, err)

	// Tenant FK trigger must reject invalid tenant
	_, err = database.Exec(`
		INSERT INTO maintenance_plans (id, tenant_id, plan_number, vehicle_id, service_type)
		VALUES ('mp-bad', 'ghost-tenant', 'MP-9999', 'veh-plan-1', 'oil_change')
	`)
	require.Error(t, err, "tenant FK trigger must reject missing tenant")

	// Verify Down rollback
	_, err = provider.Down(ctx)
	require.NoError(t, err)

	// Verify table dropped
	var tableName string
	err = database.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name='maintenance_plans'`).Scan(&tableName)
	require.ErrorIs(t, err, sql.ErrNoRows)
}
