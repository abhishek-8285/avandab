package db

import (
	"context"
	"database/sql"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	"io/fs"
	_ "modernc.org/sqlite"
	"path/filepath"
	"testing"
	"testing/fstest"
)

// TestMigration00117TelemetryParityUpAndDown proves 00117 applies and rolls
// back cleanly (Prove-It protocol #4).
func TestMigration00117TelemetryParityUpAndDown(t *testing.T) {
	content, err := Migrations.ReadFile("migrations/00117_telemetry_provider_parity.sql")
	require.NoError(t, err)

	mapFS := fstest.MapFS{
		"00117_telemetry_provider_parity.sql": &fstest.MapFile{Data: content},
	}
	var fsys fs.FS = mapFS

	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "mig.db"))
	require.NoError(t, err)
	defer database.Close()

	// Minimal pre-existing schema the migration ALTERs.
	_, err = database.Exec(`
CREATE TABLE telemetry_positions (
    id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL DEFAULT '1', imei TEXT NOT NULL,
    device_time DATETIME NOT NULL, received_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    latitude REAL NOT NULL, longitude REAL NOT NULL, provider TEXT NOT NULL DEFAULT 'own'
);
CREATE TABLE vehicle_latest_position (
    vehicle_id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL DEFAULT '1',
    device_time DATETIME NOT NULL, received_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    latitude REAL NOT NULL, longitude REAL NOT NULL
);`)
	require.NoError(t, err)

	ctx := context.Background()
	provider, err := goose.NewProvider(goose.DialectSQLite3, database, fsys)
	require.NoError(t, err)

	_, err = provider.Up(ctx)
	require.NoError(t, err)

	for _, col := range []string{"satellites", "battery_level", "external_voltage", "gsm_signal", "motion", "valid"} {
		var n int
		require.NoError(t, database.QueryRow(
			`SELECT COUNT(*) FROM pragma_table_info('telemetry_positions') WHERE name = ?`, col).Scan(&n), col)
		require.Equal(t, 1, n, "telemetry_positions missing column "+col)
		require.NoError(t, database.QueryRow(
			`SELECT COUNT(*) FROM pragma_table_info('vehicle_latest_position') WHERE name = ?`, col).Scan(&n), col)
		require.Equal(t, 1, n, "vehicle_latest_position missing column "+col)
	}
	var nFix int
	require.NoError(t, database.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('telemetry_positions') WHERE name = 'fix_time'`).Scan(&nFix))
	require.Equal(t, 1, nFix)

	_, err = provider.DownTo(ctx, 0)
	require.NoError(t, err)
}

func TestMigration00083ErrorReportsUpAndDown(t *testing.T) {
	content, err := Migrations.ReadFile("migrations/00083_error_reports_incidents.sql")
	require.NoError(t, err)

	mapFS := fstest.MapFS{
		"00083_error_reports_incidents.sql": &fstest.MapFile{Data: content},
	}
	var fsys fs.FS = mapFS

	dbPath := filepath.Join(t.TempDir(), "mig.db")
	database, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer database.Close()

	ctx := context.Background()

	_, err = database.Exec(`
CREATE TABLE roles (id INTEGER PRIMARY KEY, name TEXT);
CREATE TABLE permissions (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT NOT NULL UNIQUE,
    description TEXT
);
CREATE TABLE role_permissions (
    role_id       INTEGER NOT NULL,
    permission_id INTEGER NOT NULL,
    PRIMARY KEY (role_id, permission_id)
);`)
	require.NoError(t, err)

	provider, err := goose.NewProvider(goose.DialectSQLite3, database, fsys)
	require.NoError(t, err)

	_, err = provider.Up(ctx)
	require.NoError(t, err)

	for _, q := range []string{
		`SELECT COUNT(*) FROM error_reports`,
		`SELECT COUNT(*) FROM incidents`,
		`SELECT COUNT(*) FROM permissions WHERE name IN ('errors:read','errors:update')`,
	} {
		var n int
		require.NoError(t, database.QueryRow(q).Scan(&n), q)
	}

	_, err = provider.DownTo(ctx, 0)
	require.NoError(t, err)

	for _, table := range []string{"error_reports", "incidents"} {
		var name string
		err := database.QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table,
		).Scan(&name)
		require.ErrorIs(t, err, sql.ErrNoRows, "table %s should be dropped", table)
	}

	var perms int
	require.NoError(t, database.QueryRow(
		`SELECT COUNT(*) FROM permissions WHERE name LIKE 'errors:%'`,
	).Scan(&perms))
	require.Zero(t, perms, "seeded permissions should be removed on down")
}
