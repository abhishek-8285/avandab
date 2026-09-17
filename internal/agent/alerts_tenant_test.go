package agent

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/config"
	"transport-app/internal/events"
	"transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

// TestGetOpenAlerts_TenantIsolation is the red-proven regression test for the
// cross-tenant leak found in audit 2026-09-17: the get_open_alerts tool query
// had no tenant_id filter, so tenant A's alerts were visible to tenant B.
// Pre-fix, this test fails on the "must not see tenant B alerts" assertion.
func TestGetOpenAlerts_TenantIsolation(t *testing.T) {
	db := newAgentTestDB(t)
	defer func() { _ = db.Close() }()

	// Two tenants, one open alert each. Both are "open" + same severity so the
	// only thing that can separate them is the tenant filter.
	_, err := db.Exec(`INSERT INTO tenants (id, name, status) VALUES
		('tenant-a', 'Tenant A', 'active'),
		('tenant-b', 'Tenant B', 'active')`)
	require.NoError(t, err)

	// alerts.source is FK-constrained to alert_sources.name; seed a valid
	// source or the insert is silently rejected under foreign_keys=on.
	_, err = db.Exec(`INSERT INTO alert_sources (id, name, is_active) VALUES
		('src-test', 'system', 1) ON CONFLICT(name) DO NOTHING`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO alerts
		(id, rule_id, source, alert_type, severity, status, tenant_id, dedup_key, title, message, entity_type, entity_id, created_at, last_seen_at, occurrences)
		VALUES
		('alert-tenant-a', 'r1', 'system', 'speed', 'high', 'open', 'tenant-a', 'dk-a', 'Tenant A alert', 'A only', 'vehicle', 'veh-a', datetime('now'), datetime('now'), 1),
		('alert-tenant-b', 'r2', 'system', 'speed', 'high', 'open', 'tenant-b', 'dk-b', 'Tenant B alert', 'B only', 'vehicle', 'veh-b', datetime('now'), datetime('now'), 1)`)
	require.NoError(t, err)

	store := sqlite.NewRepository(db)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{}
	bus := events.NewInMemoryBus()
	services := service.NewServices(store, cfg, logger, bus)

	env := &ToolEnv{Services: services}
	tools := RegisterTools(env)

	var alertTool *RegisteredTool
	for _, tool := range tools {
		if tool.Name == "get_open_alerts" {
			alertTool = tool
			break
		}
	}
	require.NotNil(t, alertTool, "get_open_alerts tool must be registered")

	// Tenant A calls the tool. It must see ONLY tenant A's alert.
	ctx := shared.ContextWithTenantID(context.Background(), "tenant-a")
	res, err := alertTool.Handler(ctx, json.RawMessage(`{}`))
	require.NoError(t, err)

	assert.Contains(t, res, "alert-tenant-a", "tenant A must see its own open alerts")
	assert.NotContains(t, res, "alert-tenant-b", "tenant A must NOT see tenant B's alerts (cross-tenant leak)")
	assert.NotContains(t, res, "B only", "tenant B alert message must not leak")
}
