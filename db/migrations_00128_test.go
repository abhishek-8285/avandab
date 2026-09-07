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

// TestMigration00128EwayBillsStatusLifecycle proves 00128 applies AND rolls
// back: 'part_a'/'delivered' pass after Up (the delivery UPDATE needs them)
// and are rejected after Down.
func TestMigration00128EwayBillsStatusLifecycle(t *testing.T) {
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

	_, err = database.Exec(
		`INSERT INTO eway_bills (id, ewb_number, generation_date, valid_until, status)
		 VALUES ('b1', 'EWB1', '2026-09-01', '2026-09-10', 'part_a')`,
	)
	require.NoError(t, err, "'part_a' must pass after up")
	_, err = database.Exec(`UPDATE eway_bills SET status='delivered' WHERE id='b1'`)
	require.NoError(t, err, "'delivered' must pass after up")

	_, err = provider.DownTo(ctx, 127)
	require.NoError(t, err)

	_, err = database.Exec(
		`INSERT INTO eway_bills (id, ewb_number, generation_date, valid_until, status)
		 VALUES ('b2', 'EWB2', '2026-09-01', '2026-09-10', 'delivered')`,
	)
	require.Error(t, err, "'delivered' must be rejected after down to 127")

	_, err = provider.Up(ctx)
	require.NoError(t, err)
	assertForeignKeyCheckClean(t, database, "00128 re-up")
}
