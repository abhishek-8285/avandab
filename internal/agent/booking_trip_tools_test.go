package agent

// Integration regression for the seven booking/trip agent tools after they
// moved onto application facades (handlers-end-state ADR, Phase 3).

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

func newBookingTripToolEnv(t *testing.T) map[string]*RegisteredTool {
	t.Helper()
	db := newAgentTestDB(t)
	_, err := db.Exec(`INSERT INTO customers (id, tenant_id, name, phone, customer_code) VALUES ('cust-1','1','Acme','9991112222','CUST-1')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO routes (id, tenant_id, source, destination, distance, estimated_hours, standard_fare) VALUES ('route-1','1','Mumbai','Pune',150,3,1000)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO trips (id, tenant_id, trip_number, route_id, departure_time, status) VALUES ('trip-1','1','TR-1','route-1','2026-10-01 08:30:00','in_transit')`)
	require.NoError(t, err)
	services := service.NewServices(sqlite.NewRepository(db), &config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)), events.NewInMemoryBus())
	_ = services
	byName := map[string]*RegisteredTool{}
	for _, tl := range RegisterTools(&ToolEnv{DB: db, UserID: "agent-op"}) {
		byName[tl.Name] = tl
	}
	return byName
}

func bookingTripCtx(tenant string) context.Context {
	return WithAgentUser(shared.ContextWithTenantID(context.Background(), shared.TenantID(tenant)), "agent-op", "Agent Op")
}

func TestBookingTripTools_FacadeIntegration(t *testing.T) {
	byName := newBookingTripToolEnv(t)
	ctx := bookingTripCtx("1")

	res, err := byName["search_routes"].Handler(ctx, json.RawMessage(`{"query":"Mumbai"}`))
	require.NoError(t, err)
	assert.Contains(t, res, "Mumbai")
	assert.Contains(t, res, "standard_fare")

	res, err = byName["get_quote"].Handler(ctx, json.RawMessage(`{"from":"Mumbai","to":"Pune"}`))
	require.NoError(t, err)
	assert.Contains(t, res, "base_fare")
	assert.Contains(t, res, "total_with_gst")

	res, err = byName["search_customers"].Handler(ctx, json.RawMessage(`{"query":"Acme"}`))
	require.NoError(t, err)
	assert.Contains(t, res, "cust-1")

	res, err = byName["create_booking"].Handler(ctx, json.RawMessage(`{"customer_id":"cust-1","route_id":"route-1","pickup_date":"2026-10-02 09:00","vehicle_type":"truck","passengers":2,"price":1500}`))
	require.NoError(t, err)
	assert.Contains(t, res, "booking_number")
	var created map[string]any
	require.NoError(t, json.Unmarshal([]byte(res), &created))
	number, _ := created["booking_number"].(string)
	require.NotEmpty(t, number)

	res, err = byName["get_booking"].Handler(ctx, json.RawMessage(`{"number":`+q(number)+`}`))
	require.NoError(t, err)
	assert.Contains(t, res, "Acme")
	var bk map[string]any
	require.NoError(t, json.Unmarshal([]byte(res), &bk))
	assert.Equal(t, "Mumbai -> Pune", bk["route"])

	res, err = byName["list_trips"].Handler(ctx, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Contains(t, res, "TR-1")
	var trips []map[string]any
	require.NoError(t, json.Unmarshal([]byte(res), &trips))
	require.Len(t, trips, 1)
	assert.Equal(t, "Mumbai -> Pune", trips[0]["route"])

	res, err = byName["get_trip"].Handler(ctx, json.RawMessage(`{"number":"TR-1"}`))
	require.NoError(t, err)
	assert.Contains(t, res, "TR-1")
	assert.Contains(t, res, `"status":"in_transit"`)

	// Not-found model strings and tenant isolation.
	_, err = byName["get_booking"].Handler(ctx, json.RawMessage(`{"number":"BK-NOPE"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "booking not found")
	_, err = byName["get_trip"].Handler(ctx, json.RawMessage(`{"number":"TR-NOPE"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "trip not found")

	ctx2 := bookingTripCtx("2")
	res, err = byName["search_routes"].Handler(ctx2, json.RawMessage(`{"query":"Mumbai"}`))
	require.NoError(t, err)
	assert.Equal(t, "No routes found matching: Mumbai", res)
	res, err = byName["list_trips"].Handler(ctx2, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Contains(t, res, "No trips found")
}
