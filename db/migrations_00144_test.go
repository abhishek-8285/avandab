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

func TestMigration00144TripStartOdometerAndGateRegister(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "mig_00144.db")
	database, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer database.Close()

	ctx := context.Background()
	migFS, err := fs.Sub(Migrations, "migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectSQLite3, database, migFS)
	require.NoError(t, err)

	_, err = provider.UpTo(ctx, 144)
	require.NoError(t, err)

	assertForeignKeyCheckClean(t, database, "after 00144 up")

	// Seed prerequisite route and vehicle
	_, err = database.Exec(`
		INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, status, tenant_id)
		VALUES ('veh-gate-1', 'KA01GR1001', 'V-GR-01', 'truck', 10000, 'available', '1');
		INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare, tenant_id)
		VALUES ('route-gate-1', 'Depot A', 'Hub B', 120.0, 3.5, 5000.0, '1');
	`)
	require.NoError(t, err)

	// Insert trip with start_odometer and gate_facility_id
	_, err = database.Exec(`
		INSERT INTO trips (id, trip_number, route_id, vehicle_id, departure_time, status, tenant_id, start_odometer, close_odometer, gate_facility_id)
		VALUES ('trip-gate-1', 'TRP-GATE-01', 'route-gate-1', 'veh-gate-1', '2026-09-11 08:00:00', 'completed', '1', 54200.5, 54320.5, 'DEPOT-BLR-01')
	`)
	require.NoError(t, err)

	var startOdo, closeOdo sql.NullFloat64
	var facility sql.NullString
	err = database.QueryRowContext(ctx, `SELECT start_odometer, close_odometer, gate_facility_id FROM trips WHERE id = 'trip-gate-1'`).Scan(&startOdo, &closeOdo, &facility)
	require.NoError(t, err)
	require.True(t, startOdo.Valid)
	require.Equal(t, 54200.5, startOdo.Float64)
	require.True(t, closeOdo.Valid)
	require.Equal(t, 54320.5, closeOdo.Float64)
	require.True(t, facility.Valid)
	require.Equal(t, "DEPOT-BLR-01", facility.String)

	// Verify Down rollback
	_, err = provider.Down(ctx)
	require.NoError(t, err)

	// After down, start_odometer and gate_facility_id columns should be removed
	var dummy sql.NullFloat64
	err = database.QueryRowContext(ctx, `SELECT start_odometer FROM trips WHERE id = 'trip-gate-1'`).Scan(&dummy)
	require.Error(t, err, "column start_odometer should not exist after rollback")
}
