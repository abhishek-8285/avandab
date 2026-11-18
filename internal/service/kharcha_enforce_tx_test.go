package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/shared"
)

// The fuel-enforce gate and its audit_status pre-check must run on the same
// transaction as the status UPDATE. With a single pooled connection, the
// approval tx holds it: an out-of-tx read cannot get a connection and times
// out (surfacing "expense not found"), while a tx-scoped read succeeds and
// returns the real needs_review verdict.
func TestApproveExpense_EnforceGate_RunsOnTx(t *testing.T) {
	db := auditTestDB(t)
	svcs := auditTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), shared.DefaultTenant)
	expenseID := setupNeedsReviewClaim(t, db, svcs, ctx)

	_, err := db.Exec(`UPDATE company_config SET value = 'true' WHERE key = 'fuel.audit_enforce'`)
	require.NoError(t, err)

	db.SetMaxOpenConns(1)
	shortCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	err = svcs.Kharcha.ApproveExpense(shortCtx, expenseID, "user-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "claim flagged by fuel audit",
		"gate must run on the tx, got: %v", err)

	var status string
	require.NoError(t, db.QueryRow(`SELECT status FROM driver_expenses WHERE id = ?`, expenseID).Scan(&status))
	assert.Equal(t, "pending", status, "flagged claim must never flip to approved")
}
