package db

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// TestMigration00126FleetRegistrySOPParity proves 00126 applies AND rolls back
// (Prove-It #4) against the full migration chain.
// The points of 00126: SOP fleet-object columns exist, a vehicle with
// status 'blocked' is storable (pre-00126 CHECK rejected it), and the
// IK01/IK11 measuring tables work with tenant FK triggers.
func TestMigration00126FleetRegistrySOPParity(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "mig.db")
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

	assertForeignKeyCheckClean(t, database, "after up")

	checkSQL := ""
	require.NoError(t, database.QueryRow(
		`SELECT sql FROM sqlite_master WHERE type='table' AND name='vehicles'`,
	).Scan(&checkSQL))
	for _, want := range []string{"'blocked'", "fleet_class", "ownership", "fleet_number", "facility_id", "chassis_no", "acquisition_value", "usage_indicator"} {
		require.True(t, strings.Contains(checkSQL, want), "vehicles DDL should contain %q after up", want)
	}

	// The regression this migration fixes: 'blocked' must be writable.
	_, err = database.Exec(
		`INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, insurance_expiry, fitness_expiry, permit_expiry, status, tenant_id, fleet_class, ownership, manufacturer, facility_id)
		 VALUES ('v1', 'MH01AB1234', 'VN-001', 'truck', 1000, '2030-01-01', '2030-01-01', '2030-01-01', 'blocked', '1', 'CV', 'O', 'TATA', 'MM21000000757')`,
	)
	require.NoError(t, err, "'blocked' must pass the widened CHECK")

	// Measuring points + documents round-trip with tenant triggers enforced.
	_, err = database.Exec(
		`INSERT INTO vehicle_measuring_points (id, tenant_id, vehicle_id, kind, meas_position, annual_estimate, description)
		 VALUES ('p1', '1', 'v1', 'ODO', 'DISTANCE', 50000, 'Tata Truck MH01AB1234')`,
	)
	require.NoError(t, err)
	_, err = database.Exec(
		`INSERT INTO vehicle_measurements (id, tenant_id, point_id, counter_reading, difference_reading, total_counter_reading, read_by)
		 VALUES ('m1', '1', 'p1', 1200, 1200, 1200, 'TCS795488')`,
	)
	require.NoError(t, err)

	// Roll back ONLY 00126 (DownTo 125) and verify legacy behaviour returns.
	_, err = provider.DownTo(ctx, 125)
	require.NoError(t, err)

	assertForeignKeyCheckClean(t, database, "after down")

	var status string
	require.NoError(t, database.QueryRow(`SELECT status FROM vehicles WHERE id = 'v1'`).Scan(&status))
	require.Equal(t, "inactive", status, "down should map blocked → inactive")

	require.NoError(t, database.QueryRow(
		`SELECT sql FROM sqlite_master WHERE type='table' AND name='vehicles'`,
	).Scan(&checkSQL))
	require.False(t, strings.Contains(checkSQL, `'blocked'`), "down should restore legacy CHECK")
	require.False(t, strings.Contains(checkSQL, `fleet_class`), "down should drop SOP columns")

	var tblCount int
	require.NoError(t, database.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('vehicle_measuring_points','vehicle_measurements')`,
	).Scan(&tblCount))
	require.Equal(t, 0, tblCount, "down should drop measuring tables")
}

// assertForeignKeyCheckClean is the integrity gate for the 00126 parent-table
// rebuild: no dangling FK references may survive the copy in either direction.
func assertForeignKeyCheckClean(t *testing.T, database *sql.DB, stage string) {
	t.Helper()
	rows, err := database.Query(`PRAGMA foreign_key_check`)
	require.NoError(t, err)
	defer rows.Close()
	var violations []string
	for rows.Next() {
		var table, rowid, fktable string
		var fkrowid int64
		if err := rows.Scan(&table, &rowid, &fktable, &fkrowid); err != nil {
			// Some drivers return different shapes; any row at all is a violation.
			violations = append(violations, "unreadable violation row")
			continue
		}
		violations = append(violations, table+"/"+rowid+"->"+fktable)
	}
	require.NoError(t, rows.Err())
	require.Empty(t, violations, "foreign_key_check must be clean "+stage)
}
