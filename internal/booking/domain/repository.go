package domain

import (
	"context"
	"time"

	"transport-app/internal/booking/domain/aggregate"
	"transport-app/internal/shared"
)

// BookingReadModel represents an optimized read model combining booking, customer, and route details.
type BookingReadModel struct {
	ID               string
	BookingNumber    string
	CustomerID       string
	CustomerName     string
	CustomerCompany  string
	RouteID          string
	RouteSource      string
	RouteDestination string
	PickupDate       time.Time
	VehicleType      string
	Passengers       int64
	CargoWeight      *float64
	Price            float64
	Notes            string
	Status           string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// BookingRepository defines the contract for persisting and retrieving BookingAggregates and Read Models.
type BookingRepository interface {
	Save(ctx context.Context, b *aggregate.BookingAggregate) error
	Find(ctx context.Context, id aggregate.BookingID, tenantID shared.TenantID) (*aggregate.BookingAggregate, error)
	FindByNumber(ctx context.Context, number string, tenantID shared.TenantID) (*aggregate.BookingAggregate, error)
	FindByIdempotencyKey(ctx context.Context, key string, tenantID shared.TenantID) (*aggregate.BookingAggregate, error)
	Exists(ctx context.Context, id aggregate.BookingID, tenantID shared.TenantID) (bool, error)
	Delete(ctx context.Context, id aggregate.BookingID, tenantID shared.TenantID) error

	// Read Model Queries
	GetReadModel(ctx context.Context, id aggregate.BookingID, tenantID shared.TenantID) (BookingReadModel, error)
	SearchReadModels(ctx context.Context, tenantID shared.TenantID, query string, status string, limit int, offset int) ([]BookingReadModel, int64, error)
}
