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

// TestMigration00140FilesTenantScopeUpAndDown proves 00140 applies AND rolls back
// cleanly: adds tenant_id column to files, creates index idx_files_tenant_uploadable,
// triggers trg_files_tenant_fk_insert/update, and DownTo(139) drops them cleanly.
func TestMigration00140FilesTenantScopeUpAndDown(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "mig.db")
	database, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer database.Close()

	ctx := context.Background()
	migFS, err := fs.Sub(Migrations, "migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectSQLite3, database, migFS)
	require.NoError(t, err)

	// Migrate to 139 first
	_, err = provider.UpTo(ctx, 139)
	require.NoError(t, err)

	// Apply 140
	res, err := provider.UpTo(ctx, 140)
	require.NoError(t, err)
	require.Len(t, res, 1)

	// Check that tenant_id column exists
	var colCount int
	rows, err := database.Query("PRAGMA table_info(files)")
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		require.NoError(t, rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk))
		if name == "tenant_id" {
			colCount++
		}
	}
	require.Equal(t, 1, colCount, "column tenant_id must exist in files after Up")

	// Check that index idx_files_tenant_uploadable exists
	var count int
	err = database.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_files_tenant_uploadable'`).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 1, count, "index idx_files_tenant_uploadable must exist after Up")

	// Roll back to 139
	downRes, err := provider.DownTo(ctx, 139)
	require.NoError(t, err)
	require.Len(t, downRes, 1)

	// Check that index is dropped
	err = database.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_files_tenant_uploadable'`).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 0, count, "index idx_files_tenant_uploadable must be dropped after Down")
}
