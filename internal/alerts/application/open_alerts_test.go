package application

// Focused tests for the tenant-scoped open-alert read model extracted from
// the agent get_open_alerts tool (handlers-end-state ADR, Phase 3).

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

const openAlertsTestSchema = `
CREATE TABLE alerts (
    id TEXT PRIMARY KEY,
    source TEXT NOT NULL,
    alert_type TEXT NOT NULL,
    severity TEXT NOT NULL,
    entity_type TEXT,
    entity_id TEXT,
    title TEXT NOT NULL,
    message TEXT NOT NULL,
    status TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

func newOpenAlertsTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("open_alerts_%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	require.NoError(t, err)
	_, err = db.Exec(openAlertsTestSchema)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestOpenAlertQueries_TenantIsolationAndShape(t *testing.T) {
	db := newOpenAlertsTestDB(t)
	ctx := context.Background()
	_, err := db.Exec(`INSERT INTO alerts (id, source, alert_type, severity, entity_type, entity_id, title, message, status, tenant_id, created_at) VALUES
		('a-open', 'system', 'speeding', 'warning', 'vehicle', 'veh-a', 'A warning', 'A msg', 'open', 'tenant-a', '2026-09-01 10:00:00'),
		('a-critical', 'system', 'sos', 'critical', NULL, NULL, 'A critical', 'A crit', 'open', 'tenant-a', '2026-09-02 10:00:00'),
		('a-acked', 'system', 'speeding', 'warning', NULL, NULL, 'A acked', 'A acked', 'acknowledged', 'tenant-a', '2026-09-03 10:00:00'),
		('b-open', 'system', 'speeding', 'warning', NULL, NULL, 'B warning', 'B msg', 'open', 'tenant-b', '2026-09-04 10:00:00')`)
	require.NoError(t, err)

	q := NewOpenAlertQueries(db)
	all, err := q.List(ctx, "tenant-a", "", 20)
	require.NoError(t, err)
	require.Len(t, all, 2, "only tenant A's OPEN alerts")
	assert.Equal(t, "a-critical", all[0].ID, "newest first")
	assert.Equal(t, "a-open", all[1].ID)
	assert.Equal(t, "vehicle", *all[1].EntityType, "nullable entity fields survive scanning")
	assert.Equal(t, "veh-a", *all[1].EntityID)
	assert.Nil(t, all[0].EntityType)

	filtered, err := q.List(ctx, "tenant-a", "warning", 20)
	require.NoError(t, err)
	require.Len(t, filtered, 1)
	assert.Equal(t, "a-open", filtered[0].ID)

	// Empty/unknown tenant must see nothing (no global fallback).
	for _, tenant := range []string{"", "tenant-nope"} {
		rows, err := q.List(ctx, tenant, "", 20)
		require.NoError(t, err)
		assert.Empty(t, rows, "tenant %q", tenant)
	}

	// Wire shape mirrors the legacy tool output tags exactly.
	b, err := json.Marshal(all)
	require.NoError(t, err)
	var shape []map[string]any
	require.NoError(t, json.Unmarshal(b, &shape))
	for _, key := range []string{"id", "source", "alert_type", "severity", "entity_type", "entity_id", "title", "message", "status", "created_at"} {
		assert.Contains(t, shape[0], key)
	}

	// Missing table maps to an error (tool renders "No alerts found: <err>").
	_, err = db.Exec(`DROP TABLE alerts`)
	require.NoError(t, err)
	_, err = q.List(ctx, "tenant-a", "", 20)
	require.Error(t, err)
}

func TestOpenAlertQueries_LimitAndOrder(t *testing.T) {
	db := newOpenAlertsTestDB(t)
	ctx := context.Background()
	for i := 0; i < 25; i++ {
		_, err := db.Exec(`INSERT INTO alerts (id, source, alert_type, severity, title, message, status, tenant_id, created_at) VALUES (?, 'system', 'speeding', 'warning', 'T', 'M', 'open', 'tenant-a', ?)`,
			fmt.Sprintf("a-%02d", i), fmt.Sprintf("2026-09-01 10:%02d:00", i))
		require.NoError(t, err)
	}
	rows, err := NewOpenAlertQueries(db).List(ctx, "tenant-a", "", 20)
	require.NoError(t, err)
	require.Len(t, rows, 20, "legacy limit is 20")
	assert.Equal(t, "a-24", rows[0].ID, "newest first")

	// Nil DB is an error (the tool keeps its legacy 'database not available').
	_, err = NewOpenAlertQueries(nil).List(ctx, "tenant-a", "", 20)
	require.Error(t, err)
}
