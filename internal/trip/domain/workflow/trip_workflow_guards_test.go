package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	tripAggregate "transport-app/internal/trip/domain/aggregate"
)

// CanComplete must match TripAggregate.Complete: only delivered trips can be
// completed. Previously returned true for started, letting callers attempt a
// transition the aggregate always rejects.
func TestTripWorkflow_CanCompleteMatchesAggregate(t *testing.T) {
	w := NewTripWorkflow(nil, nil, nil, nil, nil)

	assert.False(t, w.CanComplete(tripAggregate.TripDraft))
	assert.False(t, w.CanComplete(tripAggregate.TripScheduled))
	assert.False(t, w.CanComplete(tripAggregate.TripAssigned))
	assert.False(t, w.CanComplete(tripAggregate.TripStarted))
	assert.False(t, w.CanComplete(tripAggregate.TripInTransit))
	assert.True(t, w.CanComplete(tripAggregate.TripDelivered))
	assert.False(t, w.CanComplete(tripAggregate.TripCompleted))
	assert.False(t, w.CanComplete(tripAggregate.TripCancelled))
}
