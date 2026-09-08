package aggregate

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"transport-app/internal/shared"
)

func TestTripAggregate_RecordCloseReading(t *testing.T) {
	now := time.Now()
	agg := NewTripAggregate("tr-c", shared.TenantID("1"), "TR-C", nil, "route-1", now.Add(2*time.Hour), "", now)
	assert.NoError(t, agg.Schedule(now))
	assert.NoError(t, agg.AssignDriver("driver-1", now))
	assert.NoError(t, agg.Start(now))
	assert.NoError(t, agg.ReachPickup(now))
	assert.NoError(t, agg.StartTransit(now))
	assert.NoError(t, agg.Deliver(now))

	// Reading before completion is rejected.
	assert.Error(t, agg.RecordCloseReading(125400, now))

	assert.NoError(t, agg.Complete(now))
	assert.NoError(t, agg.RecordCloseReading(125400, now.Add(time.Minute)))
	if agg.CloseOdometer == nil {
		t.Fatal("CloseOdometer nil after record")
	}
	assert.InDelta(t, 125400.0, *agg.CloseOdometer, 0.001)

	// Non-positive readings mean "not recorded".
	assert.Error(t, agg.RecordCloseReading(0, now))
	assert.Error(t, agg.RecordCloseReading(-5, now))
}

func TestNewTripAggregate(t *testing.T) {
	now := time.Now()
	tenantID := shared.TenantID("1")
	bookingID := "bk-123"

	agg := NewTripAggregate(
		"tr-123",
		tenantID,
		"TR-0001",
		&bookingID,
		"route-123",
		now.Add(2*time.Hour),
		"Standard Remarks",
		now,
	)

	assert.Equal(t, TripID("tr-123"), agg.ID)
	assert.Equal(t, TripDraft, agg.Status)
	assert.Nil(t, agg.StartedAt)
	assert.Nil(t, agg.ReachedPickupAt)
	assert.Nil(t, agg.InTransitAt)
	assert.Nil(t, agg.DeliveredAt)
	assert.Nil(t, agg.CompletedAt)
	assert.Len(t, agg.Events(), 1)
	_, ok := agg.Events()[0].(TripCreatedEvent)
	assert.True(t, ok)
}

func TestTripAggregate_ExecutionWorkflow(t *testing.T) {
	now := time.Now()
	tenantID := shared.TenantID("1")
	bookingID := "bk-123"

	agg := NewTripAggregate(
		"tr-123",
		tenantID,
		"TR-0001",
		&bookingID,
		"route-123",
		now.Add(2*time.Hour),
		"",
		now,
	)

	agg.ClearEvents()

	// Draft -> Scheduled
	err := agg.Schedule(now)
	assert.NoError(t, err)
	assert.Equal(t, TripScheduled, agg.Status)

	// Scheduled -> Assigned
	assert.NoError(t, agg.AssignDriver("driver-1", now))
	assert.Equal(t, TripAssigned, agg.Status)

	// Assigned -> Started (timeline: started_at set)
	err = agg.Start(now)
	assert.NoError(t, err)
	assert.Equal(t, TripStarted, agg.Status)
	assert.NotNil(t, agg.StartedAt)

	// Started -> Reached Pickup
	err = agg.ReachPickup(now)
	assert.NoError(t, err)
	assert.Equal(t, TripReachedPickup, agg.Status)
	assert.NotNil(t, agg.ReachedPickupAt)

	// Reached Pickup -> In Transit
	err = agg.StartTransit(now)
	assert.NoError(t, err)
	assert.Equal(t, TripInTransit, agg.Status)
	assert.NotNil(t, agg.InTransitAt)

	// In Transit -> Delivered
	err = agg.Deliver(now)
	assert.NoError(t, err)
	assert.Equal(t, TripDelivered, agg.Status)
	assert.NotNil(t, agg.DeliveredAt)
	assert.NotNil(t, agg.ArrivalTime) // ArrivalTime now set on Deliver, not Complete

	// Delivered -> Completed (timeline: completed_at set; arrival_time already set on Deliver)
	err = agg.Complete(now)
	assert.NoError(t, err)
	assert.Equal(t, TripCompleted, agg.Status)
	assert.NotNil(t, agg.CompletedAt)
	assert.NotNil(t, agg.ArrivalTime)

	// Verify all timeline events were recorded (7 workflow step events after ClearEvents)
	assert.Len(t, agg.Events(), 7)
}

func TestTripAggregate_TransitionErrors(t *testing.T) {
	now := time.Now()
	tenantID := shared.TenantID("1")

	agg := NewTripAggregate(
		"tr-123",
		tenantID,
		"TR-0001",
		nil,
		"route-123",
		now.Add(2*time.Hour),
		"",
		now,
	)
	agg.ClearEvents()

	// Cannot reach pickup from draft
	err := agg.ReachPickup(now)
	assert.Error(t, err)

	// Cannot start transit from draft
	err = agg.StartTransit(now)
	assert.Error(t, err)

	// Cannot deliver from draft
	err = agg.Deliver(now)
	assert.Error(t, err)

	// Cannot complete from draft
	err = agg.Complete(now)
	assert.Error(t, err)

	// Start without driver/vehicle assignment is allowed (from assigned or scheduled)
	// Schedule -> Start -> Complete should fail (must go through full chain)
	assert.NoError(t, agg.Schedule(now))
	assert.NoError(t, agg.AssignDriver("driver-1", now))
	assert.NoError(t, agg.Start(now))

	// Started -> Complete should fail (must reach pickup, go in transit, deliver first)
	err = agg.Complete(now)
	assert.Error(t, err)

	// Started -> In Transit should fail (must reach pickup first)
	err = agg.StartTransit(now)
	assert.Error(t, err)

	// Started -> Deliver should fail
	err = agg.Deliver(now)
	assert.Error(t, err)
}

func TestTripAggregate_TimelineEvents(t *testing.T) {
	now := time.Now()
	tenantID := shared.TenantID("1")

	agg := NewTripAggregate(
		"tr-123",
		tenantID,
		"TR-0001",
		nil,
		"route-123",
		now.Add(2*time.Hour),
		"",
		now,
	)
	agg.ClearEvents()

	assert.NoError(t, agg.Schedule(now))
	assert.NoError(t, agg.AssignDriver("driver-1", now))
	assert.NoError(t, agg.Start(now))
	assert.NoError(t, agg.ReachPickup(now))
	assert.NoError(t, agg.StartTransit(now))
	assert.NoError(t, agg.Deliver(now))
	assert.NoError(t, agg.Complete(now))

	events := agg.Events()
	assert.Len(t, events, 7)

	// Verify event types in order (0 to 6)
	_, ok1 := events[0].(TripScheduledEvent)
	assert.True(t, ok1)
	_, ok2 := events[1].(TripAssignedEvent)
	assert.True(t, ok2)
	_, ok3 := events[2].(TripStartedEvent)
	assert.True(t, ok3)
	_, ok4 := events[3].(TripReachedPickupEvent)
	assert.True(t, ok4)
	_, ok5 := events[4].(TripInTransitEvent)
	assert.True(t, ok5)
	_, ok6 := events[5].(TripDeliveredEvent)
	assert.True(t, ok6)
	_, ok7 := events[6].(TripCompletedEvent)
	assert.True(t, ok7)
}

func TestTripAggregate_AssignVehicleAndCancel(t *testing.T) {
	now := time.Now()
	tenantID := shared.TenantID("1")

	agg := NewTripAggregate(
		"tr-123",
		tenantID,
		"TR-0001",
		nil,
		"route-123",
		now.Add(2*time.Hour),
		"",
		now,
	)

	// Must assign driver before vehicle
	assert.NoError(t, agg.AssignDriver("driver-1", now))
	assert.NoError(t, agg.AssignVehicle("vehicle-99", now))
	assert.Equal(t, "vehicle-99", *agg.VehicleID)

	assert.NoError(t, agg.Cancel(now))
	assert.Equal(t, TripCancelled, agg.Status)

	// Cannot assign driver to cancelled trip
	assert.Error(t, agg.AssignDriver("driver-1", now))
	assert.Error(t, agg.AssignVehicle("vehicle-1", now))

	// Cannot schedule non-draft trip
	assert.Error(t, agg.Schedule(now))
}

func TestTripAggregate_AssignVehicleRequiresDriver(t *testing.T) {
	now := time.Now()
	tenantID := shared.TenantID("1")
	agg := NewTripAggregate("tr-123", tenantID, "TR-0001", nil, "route-123", now.Add(2*time.Hour), "", now)
	err := agg.AssignVehicle("vehicle-1", now)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "driver must be assigned")
}
