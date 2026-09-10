package booking

import (
	"time"

	"transport-app/internal/domain/types"
	"transport-app/internal/shared"
)

// BookingCreatedEvent is emitted when a new booking is created.
type BookingCreatedEvent struct {
	TenantID      shared.TenantID
	BookingID     types.BookingID
	BookingNumber string
	CustomerID    types.CustomerID
	RouteID       types.RouteID
	PickupDate    time.Time
	OccurredAt    time.Time
}

// BookingConfirmedEvent is emitted when a booking is confirmed.
type BookingConfirmedEvent struct {
	TenantID    shared.TenantID
	BookingID   types.BookingID
	ConfirmedAt time.Time
	OccurredAt  time.Time
}

// BookingCancelledEvent is emitted when a booking is cancelled.
type BookingCancelledEvent struct {
	TenantID    shared.TenantID
	BookingID   types.BookingID
	CancelledAt time.Time
	OccurredAt  time.Time
}

// BookingCompletedEvent is emitted when a booking is completed.
type BookingCompletedEvent struct {
	TenantID    shared.TenantID
	BookingID   types.BookingID
	CompletedAt time.Time
	OccurredAt  time.Time
}
