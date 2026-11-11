package application

// Focused tests for the agent revenue read model (handlers-end-state ADR,
// Phase 3): tenant-scoped totals + monthly buckets.

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/shared"
)

func TestAgentMoneyService_Revenue(t *testing.T) {
	name := fmt.Sprintf("agent_money_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE payments (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, payment_date DATETIME NOT NULL, amount REAL NOT NULL)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO payments (id, tenant_id, payment_date, amount) VALUES
		('p1','tenant-a','2026-09-02 00:00:00',100),
		('p2','tenant-a','2026-09-15 00:00:00',50),
		('p3','tenant-a','2026-10-01 00:00:00',25),
		('p4','tenant-b','2026-09-30 00:00:00',999)`)
	require.NoError(t, err)

	ctx := context.Background()
	total, monthly, err := NewAgentMoneyService(db).Revenue(ctx, shared.TenantID("tenant-a"))
	require.NoError(t, err)
	assert.Equal(t, 175.0, total)
	require.Len(t, monthly, 2)
	assert.Equal(t, "2026-09", monthly[0].Month, "ordered ascending")
	assert.Equal(t, 150.0, monthly[0].Total)
	assert.Equal(t, "2026-10", monthly[1].Month)
	assert.Equal(t, 25.0, monthly[1].Total)

	// Empty tenant: zero total, no buckets (no global fallback).
	total, monthly, err = NewAgentMoneyService(db).Revenue(ctx, shared.TenantID("tenant-nope"))
	require.NoError(t, err)
	assert.Zero(t, total)
	assert.Empty(t, monthly)
	_, _, err = NewAgentMoneyService(nil).Revenue(ctx, shared.TenantID("tenant-a"))
	require.Error(t, err)
}
