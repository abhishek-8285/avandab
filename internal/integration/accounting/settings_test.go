package accounting

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/shared"
)

func testSettingsDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:accsettings_test?mode=memory&cache=shared&_pragma=busy_timeout=5000")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS tenants (id TEXT PRIMARY KEY, name TEXT)`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS tenant_accounting_settings (
		tenant_id TEXT PRIMARY KEY, provider TEXT NOT NULL DEFAULT 'none', endpoint TEXT NOT NULL DEFAULT '',
		updated_at DATETIME NOT NULL DEFAULT (datetime('now')))`)
	require.NoError(t, err)
	for _, tid := range []string{"tenant-a", "tenant-b"} {
		_, err = db.Exec(`INSERT OR IGNORE INTO tenants (id, name) VALUES (?, ?)`, tid, tid)
		require.NoError(t, err)
	}
	return db
}

func ctxFor(tid string) context.Context {
	return shared.ContextWithTenantID(context.Background(), shared.TenantID(tid))
}

// Tenant choice wins over the global fallback.
func TestResolveConfig_TenantChoiceWins(t *testing.T) {
	db := testSettingsDB(t)
	fallback := Config{Provider: "mock", Endpoint: "https://api.accounting.example.com", Enabled: true}

	_, err := SaveSetting(ctxFor("tenant-a"), db, "tally", "http://localhost:9000")
	require.NoError(t, err)

	got := ResolveConfig(ctxFor("tenant-a"), db, fallback)
	assert.Equal(t, "tally", got.Provider)
	assert.Equal(t, "http://localhost:9000", got.Endpoint)
	assert.True(t, got.Enabled, "enabled flag must survive tenant overlay")
}

// Tenant B must never see tenant A's choice (isolation).
func TestResolveConfig_CrossTenantIsolation(t *testing.T) {
	db := testSettingsDB(t)
	fallback := Config{Provider: "mock", Enabled: true}

	_, err := SaveSetting(ctxFor("tenant-a"), db, "tally", "http://localhost:9000")
	require.NoError(t, err)

	got := ResolveConfig(ctxFor("tenant-b"), db, fallback)
	assert.Equal(t, "mock", got.Provider, "tenant-b must fall back, never inherit tenant-a")
}

// No tenant (background job) → global fallback unchanged.
func TestResolveConfig_NoTenantFallsBack(t *testing.T) {
	db := testSettingsDB(t)
	fallback := Config{Provider: "tally", Endpoint: "http://tally:9000", Enabled: true}
	got := ResolveConfig(context.Background(), db, fallback)
	assert.Equal(t, fallback, got)
}

// Unknown provider rejected; empty tenant rejected — fail closed.
func TestSaveSetting_RejectsBadInput(t *testing.T) {
	db := testSettingsDB(t)
	_, err := SaveSetting(ctxFor("tenant-a"), db, "sap", "")
	assert.Error(t, err, "unknown provider must fail, never silently keep old value")
	_, err = SaveSetting(context.Background(), db, "tally", "")
	assert.Error(t, err, "missing tenant must fail closed")
}

// Only tally is live-push; rest are CSV-import workflow.
func TestSetting_LivePushOnlyTally(t *testing.T) {
	db := testSettingsDB(t)
	for prov, live := range map[string]bool{"tally": true, "zoho": false, "busy_excel": false, "excel": false, "none": false} {
		s, err := SaveSetting(ctxFor("tenant-a"), db, prov, "")
		require.NoError(t, err)
		assert.Equal(t, live, s.LivePush, "provider %s live_push mismatch", prov)
	}
}
