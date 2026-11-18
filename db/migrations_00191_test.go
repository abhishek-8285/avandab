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

// 00191 adds tenant ownership to accounting_sync_log and rolls back cleanly.
func TestMigration00191AccountingSyncTenant(t *testing.T) {
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

	_, err = database.Exec(`INSERT INTO accounting_sync_log
		(id, tenant_id, idempotency_key, direction, entity_type, entity_id, adapter, payload_json, status)
		VALUES ('acc-1', 'tenant-1', 'k1', 'out', 'invoice', 'e1', 'mock', '{}', 'pending')`)
	require.NoError(t, err, "tenant column must exist after up")

	_, err = provider.DownTo(ctx, 180)
	require.NoError(t, err)
	_, err = database.Exec(`INSERT INTO accounting_sync_log
		(id, tenant_id, idempotency_key, direction, entity_type, entity_id, adapter, payload_json, status)
		VALUES ('acc-2', 'tenant-1', 'k2', 'out', 'invoice', 'e2', 'mock', '{}', 'pending')`)
	require.Error(t, err, "tenant column must be gone after down")

	_, err = provider.Up(ctx)
	require.NoError(t, err)
	assertForeignKeyCheckClean(t, database, "00191 re-up")
}
