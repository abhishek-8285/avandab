package application

import (
	"context"
	"errors"
	"time"

	"transport-app/internal/shared"
	"transport-app/internal/shared/ports"
	"transport-app/internal/vehicle/domain"
	"transport-app/internal/vehicle/domain/aggregate"
)

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
	Blocked            bool
	BlockedReason      string
	RCExpiry           *time.Time
	PUCExpiry          *time.Time
	Odometer           float64
	Profile            aggregate.VehicleProfile
}

type CreateVehicleUseCase struct {
	uow   ports.UnitOfWork
	idGen ports.IDGenerator
	clock ports.Clock
}

func NewCreateVehicleUseCase(uow ports.UnitOfWork, idGen ports.IDGenerator, clock ports.Clock) *CreateVehicleUseCase {
	return &CreateVehicleUseCase{uow: uow, idGen: idGen, clock: clock}
}

func (uc *CreateVehicleUseCase) Execute(ctx context.Context, cmd CreateVehicleCommand) (aggregate.VehicleID, error) {
	if cmd.RegistrationNumber == "" || cmd.VehicleNumber == "" {
		return "", errors.New("registration number and vehicle number are required")
	}

	id := aggregate.VehicleID(uc.idGen.GenerateUUID())

	v := aggregate.NewVehicleAggregate(
		id,
		cmd.TenantID,
		cmd.RegistrationNumber,
		cmd.VehicleNumber,
		cmd.VehicleType,
		cmd.Capacity,
		cmd.FuelType,
		cmd.InsuranceExpiry,
		cmd.FitnessExpiry,
		cmd.PermitExpiry,
		aggregate.VehicleAvailable,
		cmd.CurrentMileage,
		uc.clock.Now(),
	)

	if err := v.ApplyProfile(cmd.Profile, uc.clock.Now()); err != nil {
		return "", err
	}

	v.ApplyCompliance(cmd.Blocked, cmd.BlockedReason, cmd.RCExpiry, cmd.PUCExpiry, cmd.Odometer, uc.clock.Now())

	err := uc.uow.Execute(ctx, func(txCtx ports.TxContext) error {
		repo, ok := txCtx.Repositories().Vehicles().(domain.VehicleRepository)
		if !ok {
			return errors.New("failed to retrieve vehicle repository")
		}
		return repo.Save(txCtx, v)
	})

	if err != nil {
		return "", err
	}

	return id, nil
}
