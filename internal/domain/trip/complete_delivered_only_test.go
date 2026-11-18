package trip_test

import (
	"testing"

	"transport-app/internal/domain/trip"
)

// Completion is only legal from 'delivered'. The legacy Trip.CanComplete used
// to accept 'started'/'in_transit', disagreeing with TripAggregate.Complete
// and TripWorkflow.CanComplete (both delivered-only). Both handlers must
// reject a started trip.
func TestTrip_CanComplete_DeliveredOnly(t *testing.T) {
	for _, status := range []trip.TripStatus{
		trip.TripDraft,
		trip.TripScheduled,
		trip.TripAssigned,
		trip.TripStarted,
		trip.TripInTransit,
		trip.TripCompleted,
		trip.TripCancelled,
	} {
		trp := trip.Trip{Status: status}
		if err := trp.CanComplete(); err == nil {
			t.Errorf("status %q must reject completion", status)
		}
	}

	delivered := trip.Trip{Status: trip.TripDelivered}
	if err := delivered.CanComplete(); err != nil {
		t.Fatalf("delivered trip must be completeable, got: %v", err)
	}
}
