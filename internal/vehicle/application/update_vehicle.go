package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"transport-app/internal/shared"
	"transport-app/internal/shared/ports"
	"transport-app/internal/vehicle/domain"
	"transport-app/internal/vehicle/domain/aggregate"
)

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
	Blocked            bool
	BlockedReason      string
	RCExpiry           *time.Time
	PUCExpiry          *time.Time
	Odometer           float64
	Profile            aggregate.VehicleProfile
}

type UpdateVehicleUseCase struct {
	uow   ports.UnitOfWork
	clock ports.Clock
}

func NewUpdateVehicleUseCase(uow ports.UnitOfWork, clock ports.Clock) *UpdateVehicleUseCase {
	return &UpdateVehicleUseCase{uow: uow, clock: clock}
}

func (uc *UpdateVehicleUseCase) Execute(ctx context.Context, cmd UpdateVehicleCommand) error {
	// Same normalization as create: friendly error here, never a raw DB CHECK.
	cmd.FuelType = aggregate.NormalizeFuelType(cmd.FuelType)
	if !aggregate.ValidFuelType(cmd.FuelType) {
		return fmt.Errorf("invalid fuel type %q: must be one of diesel, petrol, gas, electric, cng", string(cmd.FuelType))
	}
	return uc.uow.Execute(ctx, func(txCtx ports.TxContext) error {
		repo, ok := txCtx.Repositories().Vehicles().(domain.VehicleRepository)
		if !ok {
			return errors.New("failed to retrieve vehicle repository")
		}

		v, err := repo.Find(txCtx, cmd.ID, cmd.TenantID)
		if err != nil {
			return err
		}

		err = v.UpdateDetails(
			cmd.RegistrationNumber,
			cmd.VehicleNumber,
			cmd.VehicleType,
			cmd.Capacity,
			cmd.FuelType,
			cmd.InsuranceExpiry,
			cmd.FitnessExpiry,
			cmd.PermitExpiry,
			cmd.Status,
			cmd.CurrentMileage,
			uc.clock.Now(),
		)
		if err != nil {
			return err
		}

		if err := v.ApplyProfile(cmd.Profile, uc.clock.Now()); err != nil {
			return err
		}

		v.ApplyCompliance(cmd.Blocked, cmd.BlockedReason, cmd.RCExpiry, cmd.PUCExpiry, cmd.Odometer, uc.clock.Now())

		return repo.Save(txCtx, v)
	})
}
