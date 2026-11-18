package accounting

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func tenantSyncDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`
		CREATE TABLE accounting_sync_log (
			id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL DEFAULT '', idempotency_key TEXT NOT NULL UNIQUE,
			direction TEXT NOT NULL, entity_type TEXT NOT NULL, entity_id TEXT NOT NULL, adapter TEXT NOT NULL,
			payload_json TEXT NOT NULL, external_id TEXT, status TEXT NOT NULL DEFAULT 'pending',
			attempts INTEGER NOT NULL DEFAULT 0, last_error TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE accounting_mapping (
			id TEXT PRIMARY KEY, entity_type TEXT NOT NULL, entity_id TEXT NOT NULL,
			adapter TEXT NOT NULL, external_id TEXT, created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(entity_type, entity_id, adapter)
		);
		INSERT INTO accounting_sync_log (id, tenant_id, idempotency_key, direction, entity_type, entity_id, adapter, payload_json, status)
		VALUES
		('a1', 'tenant-a', 'InvoiceGenerated:a1', 'out', 'invoice', 'a1', 'mock', '{"amount":100}', 'pending'),
		('b1', 'tenant-b', 'InvoiceGenerated:b1', 'out', 'invoice', 'b1', 'mock', '{"amount":200}', 'pending');
	`)
	require.NoError(t, err)
	return db
}

// TriggerSync may only flush the calling tenant's rows.
func TestTriggerSync_TenantScoped(t *testing.T) {
	db := tenantSyncDB(t)
	defer db.Close()

	c := &Consumer{db: db, client: &mockClient{}, cfg: Config{Provider: "mock", UseMock: true}}
	res, err := c.TriggerSync(context.Background(), "tenant-a", 0)
	require.NoError(t, err)
	require.Equal(t, 1, res.Dispatched)

	var status string
	require.NoError(t, db.QueryRow(`SELECT status FROM accounting_sync_log WHERE id='b1'`).Scan(&status))
	require.Equal(t, "pending", status, "another tenant's row must not be dispatched")
}

func TestTriggerSync_RequiresTenant(t *testing.T) {
	db := tenantSyncDB(t)
	defer db.Close()
	c := &Consumer{db: db, client: &mockClient{}, cfg: Config{Provider: "mock", UseMock: true}}
	_, err := c.TriggerSync(context.Background(), "", 0)
	require.Error(t, err)
}
