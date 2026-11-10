package application

// Characterization + extraction tests for the driver view read model moved
// out of internal/handlers/drivers.go (handlers-end-state ADR, Phase 2):
// score/tier, behaviour events, recent trips, active assigned vehicle and
// the assignment dropdown, including their legacy tenant scoping and
// best-effort error semantics.

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDriverProfileQueries_ViewData(t *testing.T) {
	db := newDriverSelfServiceTestDB(t)
	ctx := context.Background()
	mustExecDriverTest(t, db, `INSERT INTO drivers (id, driver_id, first_name, last_name, phone, score, tier, status, tenant_id)
		VALUES ('d-1', 'DRV-1', 'Profile', 'Driver', '9000000001', 87.5, 'A', 'available', '1')`)
	mustExecDriverTest(t, db, `INSERT INTO drivers (id, driver_id, first_name, last_name, phone, score, tier, status, tenant_id)
		VALUES ('d-2', 'DRV-2', 'Other', 'Tenant', '9000000002', 55, 'B', 'available', '2')`)
	mustExecDriverTest(t, db, `INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, tenant_id) VALUES
		('v-1', 'REG-B', 'MH-01-BB-2222', 'truck', '1'),
		('v-2', 'REG-A', 'MH-01-AA-1111', 'container', '1'),
		('v-t2', 'REG-T2', 'KA-02-TT-3333', 'truck', '2')`)
	// One active assignment for the tenant plus a released one that must stay
	// out of the display; the tenant-2 assignment must not leak.
	mustExecDriverTest(t, db, `INSERT INTO driver_preferred_vehicles (id, tenant_id, driver_id, vehicle_id, is_primary, unassigned_at) VALUES
		('dpv-1', '1', 'd-1', 'v-2', 1, NULL),
		('dpv-2', '1', 'd-1', 'v-1', 0, CURRENT_TIMESTAMP),
		('dpv-3', '2', 'd-2', 'v-t2', 1, NULL)`)
	// Behaviour events: six rows for the driver, five must render (newest
	// first); another driver's and another tenant's rows stay hidden.
	for i := 0; i < 6; i++ {
		mustExecDriverTest(t, db, `INSERT INTO driver_behaviour_events (id, driver_id, event_type, severity, metadata, occurred_at, created_at)
			VALUES ($1, 'd-1', 'speeding', 'high', $2, '2026-08-01 10:00:00', $3)`,
			fmt.Sprintf("ev-%d", i), fmt.Sprintf(`{"note":"event %d"}`, i),
			fmt.Sprintf("2026-08-%02d 10:00:00", i+1))
	}
	mustExecDriverTest(t, db, `INSERT INTO driver_behaviour_events (id, driver_id, event_type, severity, metadata, occurred_at, created_at)
		VALUES ('ev-other', 'd-2', 'idling', 'low', '{}', '2026-08-30 10:00:00', '2026-08-30 10:00:00')`)
	// Recent trips: six rows for the driver, five render (newest first).
	for i := 0; i < 6; i++ {
		mustExecDriverTest(t, db, `INSERT INTO trips (id, trip_number, driver_id, vehicle_id, status, created_at, tenant_id)
			VALUES ($1, $2, 'd-1', 'v-1', $3, $4, '1')`,
			fmt.Sprintf("t-%d", i), fmt.Sprintf("TRP-%d", i),
			fmt.Sprintf("status-%d", i), fmt.Sprintf("2026-08-%02d 08:00:00", i+1))
	}
	mustExecDriverTest(t, db, `INSERT INTO trips (id, trip_number, driver_id, vehicle_id, status, created_at, tenant_id)
		VALUES ('t-other', 'TRP-OTHER', 'd-2', 'v-t2', 'completed', '2026-08-30 08:00:00', '2')`)

	reads := NewDriverProfileQueries(db)
	view := reads.Load(ctx, DriverProfileQuery{DriverID: "d-1", TenantID: "1"})

	require.True(t, view.Score.Valid)
	assert.InDelta(t, 87.5, view.Score.Float64, 0.0001)
	require.True(t, view.Tier.Valid)
	assert.Equal(t, "A", view.Tier.String)

	require.Len(t, view.BehaviourEvents, 5)
	assert.Equal(t, "2026-08-06 10:00:00", view.BehaviourEvents[0].CreatedAt.Time.Format("2006-01-02 15:04:05"))
	assert.Equal(t, "speeding", view.BehaviourEvents[0].EventType)
	assert.Equal(t, "high", view.BehaviourEvents[0].Severity)
	assert.True(t, view.BehaviourEvents[0].Description.Valid)
	assert.Contains(t, view.BehaviourEvents[0].Description.String, "event 5")
	assert.False(t, view.BehaviourEvents[0].Resolved)

	require.Len(t, view.RecentTrips, 5)
	assert.Equal(t, "t-5", view.RecentTrips[0].ID)
	assert.Equal(t, "TRP-5", view.RecentTrips[0].TripNumber)
	assert.Equal(t, "status-5", view.RecentTrips[0].Status)
	assert.True(t, view.RecentTrips[0].CreatedAt.Valid)

	require.NotNil(t, view.AssignedVehicle)
	assert.Equal(t, "v-2", view.AssignedVehicle.ID)
	assert.Equal(t, "REG-A", view.AssignedVehicle.RegistrationNumber)
	assert.Equal(t, "MH-01-AA-1111", view.AssignedVehicle.VehicleNumber)
	assert.Equal(t, "container", view.AssignedVehicle.VehicleType)
	assert.True(t, view.AssignedVehicle.IsPrimary)

	require.Len(t, view.AssignVehicles, 2)
	assert.Equal(t, "v-2", view.AssignVehicles[0].ID, "dropdown ordered by registration number")
	assert.Equal(t, "REG-A", view.AssignVehicles[0].RegistrationNumber)
	assert.Equal(t, "v-1", view.AssignVehicles[1].ID)

	// The assignment + dropdown queries are tenant-scoped: acting in another
	// tenant shows that tenant's data only, while the score/tier query keeps
	// its legacy id-only predicate.
	otherTenant := reads.Load(ctx, DriverProfileQuery{DriverID: "d-1", TenantID: "2"})
	assert.Nil(t, otherTenant.AssignedVehicle)
	require.Len(t, otherTenant.AssignVehicles, 1)
	assert.Equal(t, "v-t2", otherTenant.AssignVehicles[0].ID)
	require.True(t, otherTenant.Score.Valid)
	assert.InDelta(t, 87.5, otherTenant.Score.Float64, 0.0001)

	// A missing row degrades to an empty section instead of an error.
	missing := reads.Load(ctx, DriverProfileQuery{DriverID: "d-missing", TenantID: "1"})
	assert.False(t, missing.Score.Valid)
	assert.False(t, missing.Tier.Valid)
	assert.Empty(t, missing.BehaviourEvents)
	assert.Empty(t, missing.RecentTrips)
	assert.Nil(t, missing.AssignedVehicle)

	// nil DB degrades to a renderable empty view.
	empty := NewDriverProfileQueries(nil).Load(ctx, DriverProfileQuery{DriverID: "d-1", TenantID: "1"})
	assert.False(t, empty.Score.Valid)
	assert.Nil(t, empty.AssignedVehicle)
	assert.Empty(t, empty.AssignVehicles)
}

func TestDriverProfileQueries_ReassignReplacesDisplayedVehicle(t *testing.T) {
	db := newDriverSelfServiceTestDB(t)
	ctx := context.Background()
	mustExecDriverTest(t, db, `INSERT INTO drivers (id, driver_id, first_name, last_name, phone, status, tenant_id)
		VALUES ('d-1', 'DRV-1', 'Reassign', 'Driver', '9000000001', 'available', '1')`)
	mustExecDriverTest(t, db, `INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, tenant_id) VALUES
		('v-1', 'REG-A', 'MH-01-AA-1111', 'truck', '1'),
		('v-2', 'REG-B', 'MH-01-BB-2222', 'truck', '1')`)
	mustExecDriverTest(t, db, `INSERT INTO driver_preferred_vehicles (id, tenant_id, driver_id, vehicle_id, is_primary, unassigned_at)
		VALUES ('dpv-old', '1', 'd-1', 'v-1', 1, CURRENT_TIMESTAMP)`)

	reads := NewDriverProfileQueries(db)
	view := reads.Load(ctx, DriverProfileQuery{DriverID: "d-1", TenantID: "1"})
	assert.Nil(t, view.AssignedVehicle, "released assignment must not render as assigned")

	mustExecDriverTest(t, db, `INSERT INTO driver_preferred_vehicles (id, tenant_id, driver_id, vehicle_id, is_primary)
		VALUES ('dpv-active', '1', 'd-1', 'v-2', 1)`)
	view = reads.Load(ctx, DriverProfileQuery{DriverID: "d-1", TenantID: "1"})
	require.NotNil(t, view.AssignedVehicle)
	assert.Equal(t, "v-2", view.AssignedVehicle.ID)
	assert.Equal(t, "REG-B", view.AssignedVehicle.RegistrationNumber)
}
