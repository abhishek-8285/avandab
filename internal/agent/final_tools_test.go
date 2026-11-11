package agent

// Regression for the last tools migrated off env.Services (Phase 3): facade
// wiring for availability/dashboard/assign/extend plus model-facing strings.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/shared"
)

func TestFinalTools_FacadeWiring(t *testing.T) {
	db := newAgentTestDB(t)
	byName := map[string]*RegisteredTool{}
	for _, tl := range RegisterTools(&ToolEnv{DB: db, UserID: "agent-op"}) {
		byName[tl.Name] = tl
	}
	ctx := WithAgentUser(shared.ContextWithTenantID(context.Background(), "1"), "agent-op", "Agent Op")

	res, err := byName["list_available_drivers"].Handler(ctx, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, "No drivers available right now.", res)

	res, err = byName["list_available_vehicles"].Handler(ctx, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, "No vehicles available right now.", res)

	res, err = byName["get_dashboard"].Handler(ctx, json.RawMessage(`{}`))
	require.NoError(t, err)
	var dash map[string]any
	require.NoError(t, json.Unmarshal([]byte(res), &dash))
	for _, key := range []string{"today_trips_count", "active_trips", "completed_trips", "cancelled_trips", "available_drivers", "available_vehicles", "pending_payments", "monthly_revenue"} {
		assert.Contains(t, dash, key)
	}

	// No legacy service wiring: nil EWayBill keeps its legacy model string.
	res, err = byName["extend_ewaybill"].Handler(ctx, json.RawMessage(`{"trip_id":"trip-x"}`))
	require.NoError(t, err)
	assert.Equal(t, "eway bill service not available", res)

	// nil Dispatch falls back to the DB-backed dispatcher (no env.Services).
	_, err = byName["assign_driver"].Handler(ctx, json.RawMessage(`{"trip_id":"trip-missing","driver_id":"drv-missing"}`))
	require.Error(t, err)
	_, err = byName["assign_vehicle"].Handler(ctx, json.RawMessage(`{"trip_id":"trip-missing","vehicle_id":"veh-missing"}`))
	require.Error(t, err)
}
