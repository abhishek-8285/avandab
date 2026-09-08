package realtime

import (
	"context"

	"transport-app/internal/events"
)

// AttachToBus subscribes the hub to relevant bus events and forwards them.
// Type strings use events.* constants where they exist so a rename breaks
// the build instead of silently severing the stream (the "TripStartedEvent"
// vs "trip.started" fracture that left trip updates off SSE). Dropped from
// an earlier revision: "PositionEvent" and "trip.status_changed" — zero
// producers by grep; re-add if a producer lands.
func AttachToBus(bus events.EventBus, h Broadcaster) {
	if bus == nil || h == nil {
		return
	}

	forwardTypes := []string{
		"telemetry.snapshot",
		"telemetry.alert",
		"SOSEvent",         // ingest SOS spelling (producer: IngestRawFrame)
		events.SOSEvent,    // "driver.sos_triggered" (producer: SOS handler)
		"AlertEvent",       // rule alerts (ewaybill, deviation, geofence, telemetry svc)
		events.TripCreated, // "trip.created"
		events.TripStarted, // "trip.started"
		events.TripDelivered,
		events.TripCompleted,
		"maintenance.due",
		"maintenance.cleared",
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
