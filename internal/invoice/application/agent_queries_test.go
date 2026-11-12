package application

// Focused tests for the agent invoice read model (handlers-end-state ADR,
// Phase 3): tenant isolation, number/ID lookup, unpaid filtering, balance math.

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/shared"
)

const agentInvoiceTestSchema = `
CREATE TABLE customers (id TEXT PRIMARY KEY, name TEXT);
CREATE TABLE invoices (id TEXT PRIMARY KEY, invoice_number TEXT NOT NULL, customer_id TEXT, tenant_id TEXT NOT NULL,
 total REAL NOT NULL, paid_amount REAL NOT NULL DEFAULT 0, payment_status TEXT NOT NULL DEFAULT 'pending',
 status TEXT NOT NULL DEFAULT 'outstanding', due_date DATETIME, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP);
CREATE TABLE payments (id TEXT PRIMARY KEY, invoice_id TEXT, tenant_id TEXT, amount REAL NOT NULL);
`

func newAgentInvoiceTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("agent_inv_%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	require.NoError(t, err)
	_, err = db.Exec(agentInvoiceTestSchema)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestAgentInvoiceQueries_TenantIsolationAndBalance(t *testing.T) {
	db := newAgentInvoiceTestDB(t)
	ctx := context.Background()
	_, err := db.Exec(`INSERT INTO customers (id, name) VALUES ('c1','Acme'), ('c2','Beta')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO invoices (id, invoice_number, customer_id, tenant_id, total, paid_amount, payment_status, status, due_date) VALUES
		('inv-a','INV-1','c1','tenant-a',1000,200,'partially_paid','outstanding','2026-10-01 00:00:00'),
		('inv-paid','INV-2','c1','tenant-a',500,500,'paid','outstanding',NULL),
		('inv-b','INV-3','c2','tenant-b',700,0,'pending','outstanding',NULL)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO payments (id, invoice_id, tenant_id, amount) VALUES ('p1','inv-a','tenant-a',200)`)
	require.NoError(t, err)

	q := NewAgentInvoiceQueries(db)
	inv, err := q.GetByNumber(ctx, shared.TenantID("tenant-a"), "INV-1")
	require.NoError(t, err)
	assert.Equal(t, "inv-a", inv.ID)
	assert.Equal(t, "Acme", inv.Customer)
	assert.Equal(t, 1000.0, inv.Total)
	assert.Equal(t, 200.0, inv.Paid)
	assert.Equal(t, 800.0, inv.Outstanding, "total minus summed payments")
	require.NotNil(t, inv.DueDate)
	assert.Equal(t, 2026, inv.DueDate.Year())

	// Unknown and foreign numbers are both sql.ErrNoRows (no oracle).
	_, err = q.GetByNumber(ctx, shared.TenantID("tenant-a"), "INV-3")
	assert.ErrorIs(t, err, sql.ErrNoRows)
	_, err = q.GetByNumber(ctx, shared.TenantID("tenant-a"), "INV-NOPE")
	assert.ErrorIs(t, err, sql.ErrNoRows)

	// By-id lookup powers the payment pre-flight balance read.
	byID, err := q.GetByID(ctx, shared.TenantID("tenant-a"), "inv-paid")
	require.NoError(t, err)
	assert.Equal(t, 500.0, byID.Outstanding, "legacy math: total - summed payments (paid_amount not used)")

	// Unpaid list: tenant-scoped, paid dropped, newest first, balance present.
	unpaid, err := q.ListUnpaid(ctx, shared.TenantID("tenant-a"), 50)
	require.NoError(t, err)
	require.Len(t, unpaid, 1)
	assert.Equal(t, "inv-a", unpaid[0].ID)
	assert.Equal(t, 800.0, unpaid[0].Outstanding)
	foreign, err := q.ListUnpaid(ctx, shared.TenantID("tenant-nope"), 50)
	require.NoError(t, err)
	assert.Empty(t, foreign)
	empty, err := q.ListUnpaid(ctx, shared.TenantID(""), 50)
	require.NoError(t, err)
	assert.Empty(t, empty)
}
