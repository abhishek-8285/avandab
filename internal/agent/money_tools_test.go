package agent

// Regression tests for the money tools after they moved onto the invoice,
// payment and kharcha application facades (handlers-end-state ADR, Phase 3):
// registry/RBAC, tenant isolation, legacy model-facing strings and the
// approve/reject/payment guards.

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/config"
	"transport-app/internal/events"
	"transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

func newMoneyToolEnv(t *testing.T) (map[string]*RegisteredTool, *DBShim) {
	t.Helper()
	db := newAgentTestDB(t)
	store := sqlite.NewRepository(db)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	services := service.NewServices(store, &config.Config{}, logger, events.NewInMemoryBus())
	byName := map[string]*RegisteredTool{}
	for _, tl := range RegisterTools(&ToolEnv{Services: services, UserID: "agent-op"}) {
		byName[tl.Name] = tl
	}
	return byName, &DBShim{DB: db}
}

// DBShim keeps the raw handle for seeding.
type DBShim struct{ DB *sql.DB }

func moneyToolCtx(tenant string) context.Context {
	return WithAgentUser(shared.ContextWithTenantID(context.Background(), shared.TenantID(tenant)), "agent-op", "Agent Op")
}

func TestMoneyTools_RegisteredRBACTags(t *testing.T) {
	byName, _ := newMoneyToolEnv(t)
	for name, want := range map[string][2]string{
		"get_invoice":          {"invoices", "read"},
		"list_unpaid_invoices": {"invoices", "read"},
		"record_payment":       {"payments", "create"},
		"get_revenue":          {"reports", "read"},
		"list_pending_kharcha": {"trips", "read"},
		"approve_kharcha":      {"trips", "update"},
		"reject_kharcha":       {"trips", "update"},
	} {
		tl, ok := byName[name]
		require.True(t, ok, "tool %s registered", name)
		assert.Equal(t, want[0], tl.Resource, name)
		assert.Equal(t, want[1], tl.Action, name)
	}
}

func TestMoneyTools_TenantIsolationAndModelStrings(t *testing.T) {
	byName, shim := newMoneyToolEnv(t)
	_, err := shim.DB.Exec(`INSERT INTO driver_expenses (id, tenant_id, expense_type, amount, description, status)
		VALUES ('exp-tenant-1', '1', 'toll', 250, 'Toll', 'pending')`)
	require.NoError(t, err)
	ctx1 := moneyToolCtx("1")
	ctx2 := moneyToolCtx("2")

	// Reads: tenant 1 sees its data, tenant 2 sees none (no global fallback).
	res, err := byName["list_pending_kharcha"].Handler(ctx1, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Contains(t, res, "exp-tenant-1")
	res, err = byName["list_pending_kharcha"].Handler(ctx2, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, "No pending kharcha expenses.", res)

	_, err = byName["get_invoice"].Handler(ctx1, json.RawMessage(`{"number":"INV-MISSING"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invoice not found")
	res, err = byName["list_unpaid_invoices"].Handler(ctx1, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, "No unpaid invoices. All invoices are settled.", res)
	res, err = byName["get_revenue"].Handler(ctx1, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Contains(t, res, `"total_revenue":0`)

	// Payment pre-flight: missing invoice keeps the legacy balance wrapper.
	_, err = byName["record_payment"].Handler(ctx1, json.RawMessage(`{"invoice_id":"inv-missing","amount":10,"method":"cash"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "balance unavailable")

	// Cross-tenant approve/reject cannot touch tenant 1's expense.
	_, err = byName["approve_kharcha"].Handler(ctx2, json.RawMessage(`{"expense_id":"exp-tenant-1"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already processed or not found")
	_, err = byName["reject_kharcha"].Handler(ctx2, json.RawMessage(`{"expense_id":"exp-tenant-1","reason":"x"}`))
	require.Error(t, err)

	// Reject requires a reason; approve then double-approve guard.
	_, err = byName["reject_kharcha"].Handler(ctx1, json.RawMessage(`{"expense_id":"exp-tenant-1","reason":""}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rejection reason is required")
	res, err = byName["approve_kharcha"].Handler(ctx1, json.RawMessage(`{"expense_id":"exp-tenant-1"}`))
	require.NoError(t, err)
	assert.Equal(t, "Expense exp-tenant-1 approved.", res)
	_, err = byName["approve_kharcha"].Handler(ctx1, json.RawMessage(`{"expense_id":"exp-tenant-1"}`))
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "already processed") || strings.Contains(err.Error(), "not found"))
}
