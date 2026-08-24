package events

import (
	bookingevents "transport-app/internal/domain/booking"
	tripevents "transport-app/internal/domain/trip"
)

// Canonical event-type catalog. This is the SINGLE source of truth for
// event-type strings. All publishers must use these constants and all
// subscribers must listen on them. Hand-typed strings drift and sever
// automation (Spec 09 §5.1).
const (
	BookingCreated   = "booking.created"
	BookingConfirmed = "booking.confirmed"
	BookingCancelled = "booking.cancelled"
	BookingCompleted = "booking.completed"

	TripCreated       = "trip.created"
	TripScheduled     = "trip.scheduled"
	TripAssigned      = "trip.assigned"
	TripStarted       = "trip.started"
	TripReachedPickup = "trip.reached_pickup"
	TripInTransit     = "trip.in_transit"
	TripDelivered     = "trip.delivered"
	TripCompleted     = "trip.completed"
	TripCancelled     = "trip.cancelled"

	PaymentRecorded       = "payment.recorded"
	DriverPayoutSettled   = "settlement.payout_settled"
	InvoiceGenerated      = "invoice.generated"
	RazorpayPaymentFailed = "razorpay.payment_failed"

	// Kharcha (Spec 22 §5.3): emitted post-insert by CreateExpenseWithOpts;
	// the async verifier subscribes and computes verification_state without
	// ever blocking the driver's sync path.
	ExpenseCreated = "kharcha.expense_created"

	GPSDeviationAlert = "telemetry.gps_deviation_alert"
	FuelTheftAlert    = "telemetry.fuel_theft_alert"

	GeofenceZoneBreach = "geofence.zone_breach"
)

// EventTypeOf maps a domain event struct to its canonical event-type string.
// The outbox writer consults this map BEFORE falling back to Go type-name
// derivation, so the persisted event_type in outbox_events matches exactly
// what subscribers listen on.
var EventTypeOf = map[any]string{
	bookingevents.BookingCreatedEvent{}:   BookingCreated,
	bookingevents.BookingConfirmedEvent{}: BookingConfirmed,
	bookingevents.BookingCancelledEvent{}: BookingCancelled,
	bookingevents.BookingCompletedEvent{}: BookingCompleted,

	tripevents.TripCreatedEvent{}:   TripCreated,
	tripevents.TripScheduledEvent{}: TripScheduled,
	tripevents.TripAssignedEvent{}:  TripAssigned,
	tripevents.TripStartedEvent{}:   TripStarted,
	tripevents.TripCompletedEvent{}: TripCompleted,
	tripevents.TripCancelledEvent{}: TripCancelled,
}
