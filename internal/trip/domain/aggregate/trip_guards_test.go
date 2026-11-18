package aggregate

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"transport-app/internal/shared"
)

// AssignDriver/AssignVehicle must not silently downgrade a moving trip back
// to assigned. Regression test for the state-machine downgrade bug.
func TestTripAggregate_AssignAfterStartRejected(t *testing.T) {
	now := time.Now()
	tenantID := shared.TenantID("1")

	agg := NewTripAggregate("tr-guard-1", tenantID, "TR-G1", nil, "route-1", now.Add(2*time.Hour), "", now)
	require.NoError(t, agg.Schedule(now))
	require.NoError(t, agg.AssignDriver("driver-1", now))
	require.NoError(t, agg.AssignVehicle("vehicle-1", now))
	require.NoError(t, agg.Start(now))
	assert.Equal(t, TripStarted, agg.Status)

	err := agg.AssignDriver("driver-2", now)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "after trip has started")
	assert.Equal(t, TripStarted, agg.Status)
	assert.Equal(t, "driver-1", *agg.DriverID)

	err = agg.AssignVehicle("vehicle-2", now)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "after trip has started")
	assert.Equal(t, TripStarted, agg.Status)
	assert.Equal(t, "vehicle-1", *agg.VehicleID)
}

// Pre-start assignment still works across draft/scheduled/assigned.
func TestTripAggregate_AssignBeforeStartAllowed(t *testing.T) {
	now := time.Now()
	tenantID := shared.TenantID("1")

	agg := NewTripAggregate("tr-guard-2", tenantID, "TR-G2", nil, "route-1", now.Add(2*time.Hour), "", now)
	require.NoError(t, agg.AssignDriver("driver-1", now))
	assert.Equal(t, TripAssigned, agg.Status)
	require.NoError(t, agg.AssignVehicle("vehicle-1", now))
	assert.Equal(t, "vehicle-1", *agg.VehicleID)
}

// Deliver must not force-complete a pending drop stop or clear its POD
// requirement; the stop must be closed through the POD flow first.
func TestDeliver_DoesNotForceCompleteDropStop(t *testing.T) {
	now := time.Now()
	tenantID := shared.TenantID("1")
	agg := NewTripAggregate("tr-force", tenantID, "TR-FORCE", nil, "route-1", now.Add(time.Hour), "", now)
	assert.NoError(t, agg.Schedule(now))
	assert.NoError(t, agg.AssignDriver("driver-1", now))
	assert.NoError(t, agg.Start(now))
	assert.NoError(t, agg.ReachPickup(now))
	assert.NoError(t, agg.StartTransit(now))

	agg.Stops = []TripStop{{
		ID: "stop-1", TenantID: tenantID, TripID: "tr-force", StopSequence: 1,
		StopType: StopTypeDrop, Status: StopStatusPending, PODRequired: true,
	}}

	err := agg.Deliver(now)
	assert.Error(t, err, "deliver must refuse while a POD-required stop is pending")
	assert.Equal(t, TripInTransit, agg.Status)
	assert.Equal(t, StopStatusPending, agg.Stops[0].Status)
	assert.True(t, agg.Stops[0].PODRequired, "POD requirement must not be cleared")
}
