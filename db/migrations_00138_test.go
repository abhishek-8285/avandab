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

// TestMigration00138ScaleIndexesUpAndDown proves 00138 applies AND rolls back
// cleanly: creates partial indexes on outbox_events, alerts, and driver_expenses,
// and DownTo(137) removes them.
func TestMigration00138ScaleIndexesUpAndDown(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "mig.db")
	database, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer database.Close()

	ctx := context.Background()
	migFS, err := fs.Sub(Migrations, "migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectSQLite3, database, migFS)
	require.NoError(t, err)

	// Migrate to 137 first
	_, err = provider.UpTo(ctx, 137)
	require.NoError(t, err)

	// Apply 138
	res, err := provider.UpTo(ctx, 138)
	require.NoError(t, err)
	require.Len(t, res, 1)

	// Check that indexes exist
	var count int
	err = database.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name IN ('idx_outbox_unpublished', 'idx_alerts_snooze_status', 'idx_driver_expenses_pending_fuel')`).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 3, count, "all 3 partial scale indexes must exist after migration Up")

	// Roll back to 137
	downRes, err := provider.DownTo(ctx, 137)
	require.NoError(t, err)
	require.Len(t, downRes, 1)

	// Check that indexes were dropped
	err = database.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name IN ('idx_outbox_unpublished', 'idx_alerts_snooze_status', 'idx_driver_expenses_pending_fuel')`).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 0, count, "all 3 partial scale indexes must be dropped on rollback")
}
