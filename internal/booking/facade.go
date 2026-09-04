package booking

import (
	"context"
	"transport-app/internal/booking/domain/aggregate"
	"transport-app/internal/shared"
)

// CreateBookingCommand represents an immutable command to create a booking.
type CreateBookingCommand struct {
	TenantID       shared.TenantID
	CustomerID     string
	RouteID        string
	PickupDate     string
	VehicleType    string
	Passengers     int64
	CargoWeight    *float64
	Price          float64
	Notes          string
	IdempotencyKey string
}

// BookingFacade defines the public API of the Booking module.
type BookingFacade interface {
	CreateBooking(ctx context.Context, cmd CreateBookingCommand) (aggregate.BookingID, error)
	ConfirmBooking(ctx context.Context, id aggregate.BookingID, tenantID shared.TenantID) error
	CancelBooking(ctx context.Context, id aggregate.BookingID, tenantID shared.TenantID) error
}
