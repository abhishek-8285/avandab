package application

// Focused tests for the agent customer search (handlers-end-state ADR, Phase 3).

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

func TestAgentCustomerQueries_TenantScopedSearch(t *testing.T) {
	name := fmt.Sprintf("agent_cust_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE customers (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, name TEXT, company TEXT, phone TEXT, customer_code TEXT, email TEXT, contact_person TEXT, internal_id TEXT, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO customers (id, tenant_id, name, company, phone, customer_code) VALUES
		('c-a','tenant-a','Acme Transport','Acme Logistics','9991112222','CUST-A'),
		('c-b','tenant-b','Beta Freight','Beta','8888888888','CUST-B')`)
	require.NoError(t, err)
	ctx := context.Background()
	q := NewAgentCustomerQueries(db)

	rows, err := q.Search(ctx, shared.TenantID("tenant-a"), "acme", 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "Acme Transport", rows[0].Name)
	assert.Equal(t, "Acme Logistics", rows[0].Company)
	assert.Equal(t, "CUST-A", rows[0].CustomerCode)

	other, err := q.Search(ctx, shared.TenantID("tenant-a"), "Beta", 10)
	require.NoError(t, err)
	assert.Empty(t, other, "other tenant customers must not leak")
	empty, err := q.Search(ctx, shared.TenantID(""), "acme", 10)
	require.NoError(t, err)
	assert.Empty(t, empty, "empty tenant fails closed")
}
