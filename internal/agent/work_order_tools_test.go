package agent

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/agent/rl"
	"transport-app/internal/config"
	"transport-app/internal/events"
	"transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

func newWOAgentEnv(t *testing.T) (*ToolEnv, map[string]*RegisteredTool) {
	t.Helper()
	db := newAgentTestDB(t)
	store := sqlite.NewRepository(db)
	cfg := &config.Config{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	bus := events.NewInMemoryBus()
	services := service.NewServices(store, cfg, logger, bus)

	_, err := db.Exec(`INSERT INTO vehicles
		(id, registration_number, vehicle_number, vehicle_type, capacity, status, insurance_expiry, fitness_expiry, permit_expiry, tenant_id)
		VALUES ('veh-wo','REG-WO','MH-01-WO','truck',15,'available',date('now','+1 year'),date('now','+1 year'),date('now','+1 year'),'1')`)
	require.NoError(t, err)

	env := &ToolEnv{Services: services, UserID: "agent-op", UserName: "Agent Op"}
	byName := map[string]*RegisteredTool{}
	for _, tl := range RegisterTools(env) {
		byName[tl.Name] = tl
	}
	return env, byName
}

func woAgentCtx() context.Context {
	ctx := shared.ContextWithTenantID(context.Background(), "1")
	return WithAgentUser(ctx, "agent-op", "Agent Op")
}

// RBAC tags ride on every work-order tool; execution without a checker fails closed.
func TestWorkOrderTools_RBACTags(t *testing.T) {
	_, byName := newWOAgentEnv(t)
	for name, want := range map[string][2]string{
		"list_work_orders":      {"maintenance", "read"},
		"get_work_order":        {"maintenance", "read"},
		"create_work_order":     {"maintenance", "create"},
		"transition_work_order": {"maintenance", "update"},
	} {
		tl, ok := byName[name]
		require.True(t, ok, "tool %s registered", name)
		assert.Equal(t, want[0], tl.Resource, name)
		assert.Equal(t, want[1], tl.Action, name)
	}
}

// Create → get → list → transition lifecycle through the tool handlers.
func TestWorkOrderTools_Lifecycle(t *testing.T) {
	_, byName := newWOAgentEnv(t)
	ctx := woAgentCtx()

	// Validation first.
	_, err := byName["create_work_order"].Handler(ctx, json.RawMessage(`{"title":"x"}`))
	require.Error(t, err)

	res, err := byName["create_work_order"].Handler(ctx, json.RawMessage(
		`{"vehicle_id":"veh-wo","title":"Agent brake job","cost_estimate":3000}`))
	require.NoError(t, err)
	var created map[string]any
	require.NoError(t, json.Unmarshal([]byte(res), &created))
	id, _ := created["id"].(string)
	require.NotEmpty(t, id)
	assert.Equal(t, "open", created["status"])

	res, err = byName["get_work_order"].Handler(ctx, json.RawMessage(`{"id":`+q(id)+`}`))
	require.NoError(t, err)
	assert.Contains(t, res, "Agent brake job")

	// Foreign id is invisible.
	_, err = byName["get_work_order"].Handler(ctx, json.RawMessage(`{"id":"wo-nope"}`))
	require.Error(t, err)

	res, err = byName["list_work_orders"].Handler(ctx, json.RawMessage(`{"status":"open"}`))
	require.NoError(t, err)
	assert.Contains(t, res, "Agent brake job")

	// Assign fields ride along, then done closes the books.
	_, err = byName["transition_work_order"].Handler(ctx, json.RawMessage(
		`{"id":`+q(id)+`,"status":"assigned","assignee":"Ramesh"}`))
	require.NoError(t, err)
	res, err = byName["transition_work_order"].Handler(ctx, json.RawMessage(
		`{"id":`+q(id)+`,"status":"done"}`))
	require.NoError(t, err)
	assert.Contains(t, res, "closed as done")

	res, err = byName["get_work_order"].Handler(ctx, json.RawMessage(`{"id":`+q(id)+`}`))
	require.NoError(t, err)
	assert.Contains(t, res, `"terminal":true`)

	// Unknown status rejected before touching the store.
	_, err = byName["transition_work_order"].Handler(ctx, json.RawMessage(
		`{"id":`+q(id)+`,"status":"flying"}`))
	require.Error(t, err)
}

func q(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// Mutating tools gate behind approval; reads stay direct.
func TestWorkOrderTools_Gated(t *testing.T) {
	env, byName := newWOAgentEnv(t)
	svc, err := rl.New(filepath.Join(t.TempDir(), "rl.db"))
	require.NoError(t, err)
	defer svc.Close()
	approval := NewApprovalService(svc, env)
	for _, name := range MutatingTools() {
		if tl, ok := byName[name]; ok {
			approval.Gate(name, tl.Handler)
		}
	}
	require.Contains(t, MutatingTools(), "create_work_order")
	require.Contains(t, MutatingTools(), "transition_work_order")

	agents := BuildAgentSet(byName, approval, AgentSetOptions{RequireApproval: true})
	var maint *SubAgent
	for _, sub := range agents {
		if sub.Name == "maintenance" {
			maint = sub
		}
	}
	require.NotNil(t, maint, "maintenance sub-agent exists")
	gated := map[string]bool{}
	for _, tl := range maint.Tools {
		gated[tl.Name] = strings.Contains(tl.Description, "REQUIRES ADMIN APPROVAL")
	}
	assert.True(t, gated["create_work_order"], "create gated")
	assert.True(t, gated["transition_work_order"], "transition gated")
	assert.False(t, gated["list_work_orders"], "list stays direct")
	assert.False(t, gated["get_work_order"], "get stays direct")

	// Gated submit path: approving executes the real handler.
	var createGated *RegisteredTool
	for _, tl := range maint.Tools {
		if tl.Name == "create_work_order" {
			createGated = tl
		}
	}
	require.NotNil(t, createGated)
	msg, err := createGated.Handler(woAgentCtx(), json.RawMessage(
		`{"vehicle_id":"veh-wo","title":"Gated card"}`))
	require.NoError(t, err)
	assert.Contains(t, msg, "submitted for admin approval")
	pending, err := svc.ListPendingActions()
	require.NoError(t, err)
	require.Len(t, pending, 1)
	// Approval executes under the admin's identity: their request ctx
	// carries the tenant, mirroring the RequireAPIAuth approval routes.
	action, err := approval.Approve(woAgentCtx(), pending[0].ID, "admin-1", "Admin")
	require.NoError(t, err)
	assert.Equal(t, rl.ActionExecuted, action.Status)
}
