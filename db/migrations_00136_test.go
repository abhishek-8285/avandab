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

// TestMigration00136NullBackfillUp proves 00136 converts ” sentinels to
// NULL and leaves real vehicle bindings untouched.
func TestMigration00136NullBackfillUp(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "mig.db")
	database, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer database.Close()

	ctx := context.Background()
	migFS, err := fs.Sub(Migrations, "migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectSQLite3, database, migFS)
	require.NoError(t, err)
	_, err = provider.UpTo(ctx, 135)
	require.NoError(t, err)

	_, err = database.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('t136','T136','t136')`)
	require.NoError(t, err)
	_, err = database.Exec(`INSERT INTO telemetry_positions (id, tenant_id, imei, device_time, received_at, latitude, longitude, vehicle_id)
		VALUES ('p136a','t136','imei-1','2026-09-01 10:00:00','2026-09-01 10:00:01',19.07,72.87,''),
		       ('p136b','t136','imei-1','2026-09-01 10:01:00','2026-09-01 10:01:01',19.08,72.88,'v-real')`)
	require.NoError(t, err)

	_, err = provider.UpTo(ctx, 136)
	require.NoError(t, err)

	var nulls, bound int
	require.NoError(t, database.QueryRow(
		`SELECT COUNT(*) FROM telemetry_positions WHERE vehicle_id IS NULL`).Scan(&nulls))
	require.NoError(t, database.QueryRow(
		`SELECT COUNT(*) FROM telemetry_positions WHERE vehicle_id = 'v-real'`).Scan(&bound))
	require.Equal(t, 1, nulls)
	require.Equal(t, 1, bound)

	var sentinels int
	require.NoError(t, database.QueryRow(
		`SELECT COUNT(*) FROM telemetry_positions WHERE vehicle_id = ''`).Scan(&sentinels))
	require.Equal(t, 0, sentinels)

	// Down is a documented no-op and must apply cleanly.
	_, err = provider.DownTo(ctx, 135)
	require.NoError(t, err)
	_, err = provider.Up(ctx)
	require.NoError(t, err)
}
