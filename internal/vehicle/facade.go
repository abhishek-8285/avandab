package vehicle

import (
	"context"
	"time"

	"transport-app/internal/shared"
	"transport-app/internal/vehicle/domain/aggregate"
)

// CreateVehicleCommand contains parameters to register a vehicle.
type CreateVehicleCommand struct {
	TenantID           shared.TenantID
	RegistrationNumber string
	VehicleNumber      string
	VehicleType        aggregate.VehicleType
	Capacity           int64
	FuelType           aggregate.FuelType
	InsuranceExpiry    time.Time
	FitnessExpiry      time.Time
	PermitExpiry       time.Time
	CurrentMileage     *float64
	StandardKmpl       *float64
	Blocked            bool
	BlockedReason      string
	RCExpiry           *time.Time
	PUCExpiry          *time.Time
	Odometer           float64
	Profile            aggregate.VehicleProfile
}

// UpdateVehicleCommand contains parameters to update vehicle details.
type UpdateVehicleCommand struct {
	ID                 aggregate.VehicleID
	TenantID           shared.TenantID
	RegistrationNumber string
	VehicleNumber      string
	VehicleType        aggregate.VehicleType
	Capacity           int64
	FuelType           aggregate.FuelType
	InsuranceExpiry    time.Time
	FitnessExpiry      time.Time
	PermitExpiry       time.Time
	Status             aggregate.VehicleStatus
	CurrentMileage     *float64
	StandardKmpl       *float64
	Blocked            bool
	BlockedReason      string
	RCExpiry           *time.Time
	PUCExpiry          *time.Time
	Odometer           float64
	Profile            aggregate.VehicleProfile
}

// VehicleFacade defines public entries into the Vehicle module.
type VehicleFacade interface {
	CreateVehicle(ctx context.Context, cmd CreateVehicleCommand) (aggregate.VehicleID, error)
	UpdateVehicle(ctx context.Context, cmd UpdateVehicleCommand) error
}
