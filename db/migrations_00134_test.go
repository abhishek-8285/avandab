package db

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// TestMigration00134SnapshotTsUnixUpAndDown proves 00134 applies AND rolls
// back: ts_unix backfills rows strftime understands (space-separated text),
// honestly leaves Go-String pipeline rows NULL (SQLite cannot parse the
// "+0000 UTC" suffix), and DownTo(133) drops the column (re-up restores).
func TestMigration00134SnapshotTsUnixUpAndDown(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "mig.db")
	database, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer database.Close()

	ctx := context.Background()
	migFS, err := fs.Sub(Migrations, "migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectSQLite3, database, migFS)
	require.NoError(t, err)

	// Migrate to 133 first so 134's Up runs against pre-existing rows.
	_, err = provider.UpTo(ctx, 133)
	require.NoError(t, err)

	_, err = database.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('t134', 'T134 Fleet', 't134-fleet')`)
	require.NoError(t, err)
	_, err = database.Exec(`INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, insurance_expiry, fitness_expiry, permit_expiry)
		VALUES ('v134', 'REG-134', 'MH-01-134', 'truck', 15, date('now','+1 year'), date('now','+1 year'), date('now','+1 year'))`)
	require.NoError(t, err)

	anchor := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	_, err = database.Exec(`INSERT INTO telemetry_snapshots (id, vehicle_id, timestamp, latitude, longitude)
		VALUES ('s-space', 'v134', ?, 19.07, 72.87)`, anchor.Format("2006-01-02 15:04:05"))
	require.NoError(t, err)
	_, err = database.Exec(`INSERT INTO telemetry_snapshots (id, vehicle_id, timestamp, latitude, longitude)
		VALUES ('s-gostring', 'v134', ?, 19.07, 72.87)`, "2026-09-01 12:00:00.123456789 +0000 UTC")
	require.NoError(t, err)

	_, err = provider.Up(ctx)
	require.NoError(t, err)

	var spaceUnix sql.NullInt64
	require.NoError(t, database.QueryRow(`SELECT ts_unix FROM telemetry_snapshots WHERE id = 's-space'`).Scan(&spaceUnix))
	require.True(t, spaceUnix.Valid, "space-format row must backfill")
	require.Equal(t, anchor.Unix(), spaceUnix.Int64)

	var goUnix sql.NullInt64
	require.NoError(t, database.QueryRow(`SELECT ts_unix FROM telemetry_snapshots WHERE id = 's-gostring'`).Scan(&goUnix))
	require.False(t, goUnix.Valid, "Go-String row must stay NULL, never a silently wrong epoch")

	_, err = provider.DownTo(ctx, 133)
	require.NoError(t, err)
	_, err = database.Exec(`SELECT ts_unix FROM telemetry_snapshots LIMIT 1`)
	require.Error(t, err, "DownTo(133) must drop ts_unix")

	_, err = provider.Up(ctx)
	require.NoError(t, err)
	require.NoError(t, database.QueryRow(`SELECT ts_unix FROM telemetry_snapshots WHERE id = 's-space'`).Scan(&spaceUnix))
	require.True(t, spaceUnix.Valid, "re-up must restore the column and backfill")
}
