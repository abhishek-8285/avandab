package domain

import (
	"context"
)

type CustomerRepository interface {
	SaveQuote(ctx context.Context, tenantID string, quote *Quote) error
	GetQuote(ctx context.Context, tenantID, quoteID string) (*Quote, error)
	MarkQuoteConverted(ctx context.Context, tenantID, quoteID string) error

	CreateBookingWithDetails(ctx context.Context, tenantID string, bookingMap map[string]interface{}, detailsMap map[string]interface{}) error
	GetBookingByIdempotencyKey(ctx context.Context, tenantID, idempotencyKey string) (string, error)
	CancelCustomerBooking(ctx context.Context, tenantID, customerID, bookingID, reason string) error

	GetCustomerTrackingProjection(ctx context.Context, tenantID, customerID, bookingID string) (*CustomerBookingTrackingProjection, error)
	ListCustomerBookings(ctx context.Context, tenantID, customerID string, limit, offset int) ([]CustomerBookingTrackingProjection, error)
}
