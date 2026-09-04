package trip

import (
	"context"
	"time"

	"transport-app/internal/shared"
	"transport-app/internal/trip/domain/aggregate"
)

// CreateTripCommand represents an immutable command to schedule a new trip.
type CreateTripCommand struct {
	TenantID       shared.TenantID
	BookingID      *string
	RouteID        string
	DepartureTime  time.Time
	Remarks        string
	IdempotencyKey string
}

// TripFacade defines the public API of the Trip module.
type TripFacade interface {
	CreateTrip(ctx context.Context, cmd CreateTripCommand) (aggregate.TripID, error)
	StartTrip(ctx context.Context, id aggregate.TripID, tenantID shared.TenantID) error
	CompleteTrip(ctx context.Context, id aggregate.TripID, tenantID shared.TenantID) error
	CancelTrip(ctx context.Context, id aggregate.TripID, tenantID shared.TenantID) error
}
