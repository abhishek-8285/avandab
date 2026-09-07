package domain

import (
	"context"
	"time"

	"transport-app/internal/shared"
	"transport-app/internal/vehicle/domain/aggregate"
)

// VehicleReadModel optimized for queries.
type VehicleReadModel struct {
	ID                 string
	RegistrationNumber string
	VehicleNumber      string
	VehicleType        string
	Capacity           int64
	FuelType           string
	InsuranceExpiry    time.Time
	FitnessExpiry      time.Time
	PermitExpiry       time.Time
	Status             string
	CurrentMileage     *float64
	Blocked            bool
	BlockedReason      string
	RCExpiry           *time.Time
	PUCExpiry          *time.Time
	Odometer           float64
	Profile            aggregate.VehicleProfile
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// VehicleRepository defines DDD persistence rules for vehicles.
type VehicleRepository interface {
	Save(ctx context.Context, v *aggregate.VehicleAggregate) error
	Find(ctx context.Context, id aggregate.VehicleID, tenantID shared.TenantID) (*aggregate.VehicleAggregate, error)
	GetReadModel(ctx context.Context, id aggregate.VehicleID, tenantID shared.TenantID) (VehicleReadModel, error)
	SearchReadModels(ctx context.Context, tenantID shared.TenantID, query string, status string, limit int, offset int) ([]VehicleReadModel, int64, error)
}
