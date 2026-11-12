package application

// Focused tests for the agent kharcha facade (handlers-end-state ADR, Phase
// 3): pending list tenant isolation and the approve/reject state machines
// (fuel gate, settlement deduction, audit row).

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

const agentKharchaTestSchema = `
CREATE TABLE trips (id TEXT PRIMARY KEY, trip_number TEXT);
CREATE TABLE drivers (id TEXT PRIMARY KEY, first_name TEXT, last_name TEXT);
CREATE TABLE driver_expenses (id TEXT PRIMARY KEY, trip_id TEXT, driver_id TEXT, expense_type TEXT NOT NULL,
 category TEXT, amount REAL NOT NULL, description TEXT, status TEXT, tenant_id TEXT NOT NULL,
 approved_by TEXT, approved_at DATETIME, rejected_reason TEXT, audit_status TEXT, created_at DATETIME DEFAULT CURRENT_TIMESTAMP);
CREATE TABLE driver_settlements (id TEXT PRIMARY KEY, trip_id TEXT, driver_id TEXT, advances_kharcha REAL DEFAULT 0, net_payout REAL DEFAULT 0);
CREATE TABLE settlement_lines (id TEXT PRIMARY KEY, settlement_id TEXT, trip_id TEXT, line_type TEXT, label TEXT, amount REAL, ref_id TEXT, created_at DATETIME DEFAULT CURRENT_TIMESTAMP);
CREATE TABLE company_config (tenant_id TEXT, key TEXT, value TEXT, PRIMARY KEY (tenant_id, key));
CREATE TABLE audit_logs (id TEXT PRIMARY KEY, user_id TEXT, action TEXT, table_name TEXT, record_id TEXT, old_values TEXT, new_values TEXT, ip_address TEXT, location TEXT, tenant_id TEXT, created_at DATETIME DEFAULT CURRENT_TIMESTAMP);
`

func newAgentKharchaTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("agent_kh_%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	require.NoError(t, err)
	_, err = db.Exec(agentKharchaTestSchema)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedAgentKharcha(t *testing.T, db *sql.DB, tenant string) {
	t.Helper()
	_, err := db.Exec(`INSERT OR IGNORE INTO trips (id, trip_number) VALUES ('trip-1','TRP-1')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT OR IGNORE INTO drivers (id, first_name, last_name) VALUES ('drv-1','Asha','Rao')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO driver_expenses (id, trip_id, driver_id, expense_type, category, amount, description, status, tenant_id)
		VALUES ('exp-1', 'trip-1', 'drv-1', 'toll', 'toll', 250, 'Toll', 'pending', ?)`, tenant)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO driver_settlements (id, trip_id, driver_id, advances_kharcha, net_payout) VALUES ('stl-1','trip-1','drv-1', 10, 1000)`)
	require.NoError(t, err)
}

func TestAgentExpenseStore_ListPendingTenantIsolation(t *testing.T) {
	db := newAgentKharchaTestDB(t)
	ctx := context.Background()
	seedAgentKharcha(t, db, "tenant-a")
	store := NewAgentExpenseStore(db)

	rows, err := store.ListPending(ctx, shared.TenantID("tenant-a"))
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "exp-1", rows[0].ID)
	assert.Equal(t, "TRP-1", rows[0].TripNumber)
	assert.Equal(t, "Asha Rao", rows[0].DriverName)
	assert.Equal(t, 250.0, rows[0].Amount)

	other, err := store.ListPending(ctx, shared.TenantID("tenant-b"))
	require.NoError(t, err)
	assert.Empty(t, other)
}

func TestAgentExpenseStore_ApproveStateMachine(t *testing.T) {
	db := newAgentKharchaTestDB(t)
	ctx := context.Background()
	seedAgentKharcha(t, db, "tenant-a")
	store := NewAgentExpenseStore(db)

	require.NoError(t, store.Approve(ctx, shared.TenantID("tenant-a"), "exp-1", "admin-1"))
	var status, approvedBy string
	require.NoError(t, db.QueryRow(`SELECT status, COALESCE(approved_by,'') FROM driver_expenses WHERE id='exp-1'`).Scan(&status, &approvedBy))
	assert.Equal(t, "approved", status)
	assert.Equal(t, "admin-1", approvedBy)

	var advances, payout float64
	require.NoError(t, db.QueryRow(`SELECT advances_kharcha, net_payout FROM driver_settlements WHERE id='stl-1'`).Scan(&advances, &payout))
	assert.Equal(t, 260.0, advances, "approved amount added to settlement advances")
	assert.Equal(t, 750.0, payout, "net payout reduced by the approved amount")
	var lines int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM settlement_lines WHERE settlement_id='stl-1'`).Scan(&lines))
	assert.Equal(t, 1, lines)
	var audits int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action='approve_kharcha'`).Scan(&audits))
	assert.Equal(t, 1, audits)

	// Double approval is rejected by the pending guard.
	err := store.Approve(ctx, shared.TenantID("tenant-a"), "exp-1", "admin-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already processed or not found")

	// Cross-tenant approval cannot see the row.
	err = store.Approve(ctx, shared.TenantID("tenant-b"), "exp-1", "admin-1")
	require.Error(t, err)
}

func TestAgentExpenseStore_FuelGateAndReject(t *testing.T) {
	db := newAgentKharchaTestDB(t)
	ctx := context.Background()
	seedAgentKharcha(t, db, "tenant-a")
	store := NewAgentExpenseStore(db)

	// Enforce mode + needs_review blocks approval before the status flip.
	_, err := db.Exec(`INSERT INTO company_config (tenant_id, key, value) VALUES ('tenant-a','fuel.audit_enforce','true')`)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE driver_expenses SET audit_status='needs_review' WHERE id='exp-1'`)
	require.NoError(t, err)
	err = store.Approve(ctx, shared.TenantID("tenant-a"), "exp-1", "admin-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fuel audit")

	// Reject requires a reason, then flips only pending rows.
	err = store.Reject(ctx, shared.TenantID("tenant-a"), "exp-1", "admin-1", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rejection reason is required")
	require.NoError(t, store.Reject(ctx, shared.TenantID("tenant-a"), "exp-1", "admin-1", "no receipt"))
	var status, reason string
	require.NoError(t, db.QueryRow(`SELECT status, COALESCE(rejected_reason,'') FROM driver_expenses WHERE id='exp-1'`).Scan(&status, &reason))
	assert.Equal(t, "rejected", status)
	assert.Equal(t, "no receipt", reason)
	var audits int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action='reject_kharcha'`).Scan(&audits))
	assert.Equal(t, 1, audits)
	err = store.Reject(ctx, shared.TenantID("tenant-a"), "exp-1", "admin-1", "again")
	require.Error(t, err)
}
