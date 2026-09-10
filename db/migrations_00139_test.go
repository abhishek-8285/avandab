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

// TestMigration00139SessionAndKPIIndexesUpAndDown proves 00139 applies AND rolls back
// cleanly: creates indexes on sessions, vehicles, drivers, invoices, payments, and audit_logs,
// and DownTo(138) drops them cleanly.
func TestMigration00139SessionAndKPIIndexesUpAndDown(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "mig.db")
	database, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer database.Close()

	ctx := context.Background()
	migFS, err := fs.Sub(Migrations, "migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectSQLite3, database, migFS)
	require.NoError(t, err)

	// Migrate to 138 first
	_, err = provider.UpTo(ctx, 138)
	require.NoError(t, err)

	// Apply 139
	res, err := provider.UpTo(ctx, 139)
	require.NoError(t, err)
	require.Len(t, res, 1)

	indexes := []string{
		"idx_sessions_token_hash",
		"idx_sessions_expires_at",
		"idx_vehicles_tenant_status",
		"idx_drivers_tenant_status",
		"idx_invoices_tenant_payment_status",
		"idx_payments_tenant_date",
		"idx_audit_logs_record",
		"idx_audit_logs_created_at",
	}

	// Check that indexes exist
	for _, idx := range indexes {
		var count int
		err = database.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, idx).Scan(&count)
		require.NoError(t, err, "querying index %s", idx)
		require.Equal(t, 1, count, "index %s must exist after migration Up", idx)
	}

	// Roll back to 138
	downRes, err := provider.DownTo(ctx, 138)
	require.NoError(t, err)
	require.Len(t, downRes, 1)

	// Check that indexes were dropped
	for _, idx := range indexes {
		var count int
		err = database.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, idx).Scan(&count)
		require.NoError(t, err, "querying index %s after rollback", idx)
		require.Equal(t, 0, count, "index %s must be dropped on rollback", idx)
	}
}
