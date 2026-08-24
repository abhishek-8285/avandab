package realtime

import (
	"context"

	"transport-app/internal/events"
)

// AttachToBus subscribes the hub to relevant bus events and forwards them.
// Events forwarded: telemetry.snapshot, trip.status_changed,
// maintenance.due, maintenance.cleared, PositionEvent, TripStartedEvent,
// TripCompletedEvent.
func AttachToBus(bus events.EventBus, h *Hub) {
	if bus == nil || h == nil {
		return
	}

	forwardTypes := []string{
		"telemetry.snapshot",
		"trip.status_changed",
		"maintenance.due",
		"maintenance.cleared",
		"PositionEvent",
		"TripStartedEvent",
		"TripCompletedEvent",
		// Spec 22 S5 — bookings kanban live sync (≤2s cross-user).
		"booking.created",
		"booking.confirmed",
		"booking.cancelled",
		"booking.completed",
	}

	for _, eventType := range forwardTypes {
		et := eventType // capture
		bus.Subscribe(et, func(ctx context.Context, e events.Event) error {
			h.Publish(ctx, e)
			return nil
		})
	}
}
