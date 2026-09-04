package aggregate

import (
	"errors"
	"time"

	"transport-app/internal/shared"
)

type BookingID string
type BookingStatus string

const (
	BookingDraft     BookingStatus = "draft"
	BookingPending   BookingStatus = "pending"
	BookingConfirmed BookingStatus = "confirmed"
	BookingCancelled BookingStatus = "cancelled"
	BookingCompleted BookingStatus = "completed"
)

// BookingAggregate represents the consistency boundary for a single Booking.
type BookingAggregate struct {
	ID            BookingID
	TenantID      shared.TenantID
	BookingNumber string
	CustomerID    string
	RouteID       string
	PickupDate    time.Time
	VehicleType   string
	Passengers    int64
	CargoWeight   *float64
	Price         shared.Money
	Notes         string
	Status        BookingStatus
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Version       int64

	// IdempotencyKey dedupes retried creates (empty = no key).
	IdempotencyKey string

	events []any
}

// NewBookingAggregate constructs a new BookingAggregate in 'pending' status.
func NewBookingAggregate(
	id BookingID,
	tenantID shared.TenantID,
	bookingNumber string,
	customerID string,
	routeID string,
	pickupDate time.Time,
	vehicleType string,
	passengers int64,
	cargoWeight *float64,
	price shared.Money,
	notes string,
	now time.Time,
) *BookingAggregate {
	b := &BookingAggregate{
		ID:            id,
		TenantID:      tenantID,
		BookingNumber: bookingNumber,
		CustomerID:    customerID,
		RouteID:       routeID,
		PickupDate:    pickupDate,
		VehicleType:   vehicleType,
		Passengers:    passengers,
		CargoWeight:   cargoWeight,
		Price:         price,
		Notes:         notes,
		Status:        BookingPending,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	b.RecordEvent(BookingCreatedEvent{
		BookingID:     id,
		TenantID:      tenantID,
		BookingNumber: bookingNumber,
		OccurredAt:    now,
	})

	return b
}

// Confirm transition validation.
func (b *BookingAggregate) Confirm(now time.Time) error {
	if b.Status == BookingCancelled {
		return errors.New("cancelled bookings cannot be confirmed")
	}
	if b.Status != BookingPending {
		return errors.New("only pending bookings can be confirmed")
	}

	b.Status = BookingConfirmed
	b.UpdatedAt = now

	b.RecordEvent(BookingConfirmedEvent{
		BookingID:  b.ID,
		TenantID:   b.TenantID,
		OccurredAt: now,
	})
	return nil
}

// Complete transitions a confirmed booking to completed.
func (b *BookingAggregate) Complete(now time.Time) error {
	if b.Status != BookingConfirmed {
		return errors.New("only confirmed bookings can be completed")
	}

	b.Status = BookingCompleted
	b.UpdatedAt = now

	b.RecordEvent(BookingCompletedEvent{
		BookingID:  b.ID,
		TenantID:   b.TenantID,
		OccurredAt: now,
	})
	return nil
}

// Cancel transition validation.
func (b *BookingAggregate) Cancel(now time.Time) error {
	if b.Status == BookingCompleted {
		return errors.New("completed bookings cannot be cancelled")
	}

	b.Status = BookingCancelled
	b.UpdatedAt = now

	b.RecordEvent(BookingCancelledEvent{
		BookingID:  b.ID,
		TenantID:   b.TenantID,
		OccurredAt: now,
	})
	return nil
}

// Update updates booking details.
func (b *BookingAggregate) Update(
	customerID string,
	routeID string,
	pickupDate time.Time,
	vehicleType string,
	passengers int64,
	cargoWeight *float64,
	price shared.Money,
	notes string,
	now time.Time,
) error {
	if b.Status == BookingCancelled || b.Status == BookingCompleted {
		return errors.New("cannot update cancelled or completed bookings")
	}

	b.CustomerID = customerID
	b.RouteID = routeID
	b.PickupDate = pickupDate
	b.VehicleType = vehicleType
	b.Passengers = passengers
	b.CargoWeight = cargoWeight
	b.Price = price
	b.Notes = notes
	b.UpdatedAt = now

	b.RecordEvent(BookingUpdatedEvent{
		BookingID:  b.ID,
		TenantID:   b.TenantID,
		OccurredAt: now,
	})
	return nil
}

// SetIdempotencyKey attaches the client-supplied dedupe key before Save.
func (b *BookingAggregate) SetIdempotencyKey(key string) {
	b.IdempotencyKey = key
}

// Events returns collected domain events.
func (b *BookingAggregate) Events() []any {
	return b.events
}

// ClearEvents clears the collected domain events.
func (b *BookingAggregate) ClearEvents() {
	b.events = nil
}

// RecordEvent records a domain event.
func (b *BookingAggregate) RecordEvent(event any) {
	b.events = append(b.events, event)
}

// Events definitions
type BookingCreatedEvent struct {
	BookingID     BookingID
	TenantID      shared.TenantID
	BookingNumber string
	OccurredAt    time.Time
}

type BookingConfirmedEvent struct {
	BookingID  BookingID
	TenantID   shared.TenantID
	OccurredAt time.Time
}

type BookingCancelledEvent struct {
	BookingID  BookingID
	TenantID   shared.TenantID
	OccurredAt time.Time
}

type BookingUpdatedEvent struct {
	BookingID  BookingID
	TenantID   shared.TenantID
	OccurredAt time.Time
}

type BookingCompletedEvent struct {
	BookingID  BookingID
	TenantID   shared.TenantID
	OccurredAt time.Time
}
