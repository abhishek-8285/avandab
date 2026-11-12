package application

// Focused read-model test for the agent trip facade (handlers-end-state ADR,
// Phase 3); list runs through the clean ListTripsUseCase and is covered by the
// agent-tool integration test.

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

func TestAgentTripService_GetByNumberTenantScoped(t *testing.T) {
	name := fmt.Sprintf("agent_trip_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`
		CREATE TABLE routes (id TEXT PRIMARY KEY, source TEXT, destination TEXT);
		CREATE TABLE drivers (id TEXT PRIMARY KEY, first_name TEXT, last_name TEXT);
		CREATE TABLE vehicles (id TEXT PRIMARY KEY, vehicle_number TEXT);
		CREATE TABLE trips (id TEXT PRIMARY KEY, trip_number TEXT, booking_id TEXT, driver_id TEXT, vehicle_id TEXT,
		 route_id TEXT, departure_time DATETIME, status TEXT, tenant_id TEXT)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO routes VALUES ('r1','Mumbai','Pune');
		INSERT INTO drivers VALUES ('d1','Asha','Rao'); INSERT INTO vehicles VALUES ('v1','MH-01-AB');
		INSERT INTO trips VALUES ('t1','TR-1',NULL,'d1','v1','r1','2026-10-01 08:30:00','in_transit','tenant-a')`)
	require.NoError(t, err)
	ctx := context.Background()
	svc := NewAgentTripService(db)

	td, err := svc.GetByNumber(ctx, shared.TenantID("tenant-a"), "TR-1")
	require.NoError(t, err)
	assert.Equal(t, "TR-1", td.Number)
	assert.Equal(t, "Mumbai -> Pune", td.Route)
	assert.Equal(t, "Asha Rao", td.Driver)
	assert.Equal(t, "MH-01-AB", td.Vehicle)
	assert.Equal(t, "2026-10-01 08:30", td.Departure)
	assert.Empty(t, td.BookingID)

	_, err = svc.GetByNumber(ctx, shared.TenantID("tenant-b"), "TR-1")
	assert.ErrorIs(t, err, sql.ErrNoRows, "foreign trip invisible (tool maps to trip not found)")
	_, err = svc.GetByNumber(ctx, shared.TenantID("tenant-a"), "TR-NOPE")
	assert.ErrorIs(t, err, sql.ErrNoRows)
}
