package booking

import (
	"context"

	"transport-app/internal/domain/types"
)

// BookingRepository defines the interface for booking persistence.
type BookingRepository interface {
	CreateBooking(ctx context.Context, booking Booking) (Booking, error)
	GetBookingByID(ctx context.Context, id types.BookingID) (BookingWithJoins, error)
	GetBookingByNumber(ctx context.Context, number string) (BookingWithJoins, error)
	UpdateBooking(ctx context.Context, booking Booking) (Booking, error)
	UpdateBookingStatus(ctx context.Context, id types.BookingID, status BookingStatus) (Booking, error)
	DeleteBooking(ctx context.Context, id types.BookingID) error
	SearchBookings(ctx context.Context, query string, status string, limit, offset int) ([]BookingWithJoins, error)
	ListBookingsByCustomer(ctx context.Context, customerID types.CustomerID, limit int) ([]BookingWithJoins, error)
	CountBookings(ctx context.Context, query string, status string) (int64, error)
	CountBookingsByDay(ctx context.Context) ([]BookingsByDay, error)
}

// BookingsByDay is a single day's booking count.
type BookingsByDay struct {
	Day   string
	Count int64
}

// BookingWithJoins includes customer and route details.
type BookingWithJoins struct {
	Booking
	CustomerName     string
	CustomerCompany  *string
	RouteSource      string
	RouteDestination string
}
