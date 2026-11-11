package application

// Focused test for the extracted onboarding tenant display-name sync
// (handlers-end-state ADR, Phase 2).

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestSweepSyncTenantDisplayName(t *testing.T) {
	name := fmt.Sprintf("sweep_tenant_name_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE tenants (id TEXT PRIMARY KEY, name TEXT, updated_at DATETIME)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO tenants (id, name) VALUES ('tenant-a', 'Old Name')`)
	require.NoError(t, err)

	svc := NewTenantAdminService(db)
	require.NoError(t, svc.SyncTenantDisplayName(context.Background(), "tenant-a", "New Name"))
	var got string
	require.NoError(t, db.QueryRow(`SELECT name FROM tenants WHERE id = 'tenant-a'`).Scan(&got))
	assert.Equal(t, "New Name", got)
	// Unknown tenant is a no-op (no rows affected, no error) — best-effort contract.
	assert.NoError(t, svc.SyncTenantDisplayName(context.Background(), "tenant-missing", "X"))
	assert.NoError(t, NewTenantAdminService(nil).SyncTenantDisplayName(context.Background(), "tenant-a", "X"))
}
