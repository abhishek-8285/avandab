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

func TestMigration00142FuelIssues(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "mig_00142.db")
	database, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer database.Close()

	ctx := context.Background()
	migFS, err := fs.Sub(Migrations, "migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectSQLite3, database, migFS)
	require.NoError(t, err)

	_, err = provider.Up(ctx)
	require.NoError(t, err)

	assertForeignKeyCheckClean(t, database, "after 00142 up")

	// Seed tenant, vehicles, and measuring point
	_, err = database.Exec(`
		INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, status, tenant_id, fleet_class)
		VALUES ('station-1', 'STATION-KA01', 'FS-01', 'truck', 50000, 'available', '1', 'FS'),
		       ('truck-1', 'KA01TR1234', 'TR-01', 'truck', 10000, 'available', '1', 'CV')
	`)
	require.NoError(t, err)

	_, err = database.Exec(`
		INSERT INTO vehicle_measuring_points (id, tenant_id, vehicle_id, kind, meas_position, unit, description)
		VALUES ('pump-1', '1', 'station-1', 'PUMP', 'PUMP', 'L', 'Diesel Dispenser 1')
	`)
	require.NoError(t, err)

	// Valid fuel issue insert
	_, err = database.Exec(`
		INSERT INTO fuel_issues (id, tenant_id, issue_number, fuel_station_id, pump_point_id, vehicle_id, opening_reading, closing_reading, litres_issued, vehicle_odometer, rate_per_litre, total_cost, created_by)
		VALUES ('fi-1', '1', 'SLIP-1001', 'station-1', 'pump-1', 'truck-1', 12000.0, 12150.0, 150.0, 45200.0, 94.50, 14175.0, 'operator-1')
	`)
	require.NoError(t, err)

	// Tenant FK trigger enforces valid tenant
	_, err = database.Exec(`
		INSERT INTO fuel_issues (id, tenant_id, issue_number, fuel_station_id, vehicle_id, opening_reading, closing_reading, litres_issued, created_by)
		VALUES ('fi-bad', 'ghost-tenant', 'SLIP-9999', 'station-1', 'truck-1', 100, 200, 100, 'op')
	`)
	require.Error(t, err, "tenant FK trigger must reject missing tenant")

	// Verify Down rollback
	_, err = provider.Down(ctx)
	require.NoError(t, err)

	var count int
	err = database.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='fuel_issues'`).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 0, count, "fuel_issues table must be dropped after down")

	assertForeignKeyCheckClean(t, database, "after 00142 down")
}
