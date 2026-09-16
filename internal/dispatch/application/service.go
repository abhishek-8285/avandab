package application

import (
	"context"

	dispatchdomain "transport-app/internal/dispatch/domain"
	"transport-app/internal/shared"
	tripapp "transport-app/internal/trip/application"
	"transport-app/internal/trip/domain/aggregate"
)

type AssignDriverCommand struct {
	TenantID            shared.TenantID
	ActorID             string
	TripID              aggregate.TripID
	DriverID            string
	OverrideMaintenance bool
	OverrideReason      string
}

type AssignVehicleCommand struct {
	TenantID            shared.TenantID
	ActorID             string
	TripID              aggregate.TripID
	VehicleID           string
	OverrideMaintenance bool
	OverrideReason      string
}

type Service struct {
	assignDriver  *tripapp.AssignDriverUseCase
	assignVehicle *tripapp.AssignVehicleUseCase
}

func NewService(assignDriver *tripapp.AssignDriverUseCase, assignVehicle *tripapp.AssignVehicleUseCase) *Service {
	return &Service{assignDriver: assignDriver, assignVehicle: assignVehicle}
}

func (s *Service) AssignDriver(ctx context.Context, cmd AssignDriverCommand) error {
	if cmd.TenantID == "" {
		return dispatchdomain.ErrTenantRequired
	}
	if cmd.TripID == "" || cmd.DriverID == "" {
		return dispatchdomain.ErrTargetRequired
	}
	return s.assignDriver.Execute(ctx, tripapp.AssignDriverCommand{
		TripID: cmd.TripID, DriverID: cmd.DriverID, TenantID: cmd.TenantID,
		OverrideMaintenance: cmd.OverrideMaintenance, OverrideReason: cmd.OverrideReason,
	})
}

func (s *Service) AssignVehicle(ctx context.Context, cmd AssignVehicleCommand) error {
	if cmd.TenantID == "" {
		return dispatchdomain.ErrTenantRequired
	}
	if cmd.TripID == "" || cmd.VehicleID == "" {
		return dispatchdomain.ErrTargetRequired
	}
	return s.assignVehicle.Execute(ctx, tripapp.AssignVehicleCommand{
		TripID: cmd.TripID, VehicleID: cmd.VehicleID, TenantID: cmd.TenantID,
		OverrideMaintenance: cmd.OverrideMaintenance, OverrideReason: cmd.OverrideReason,
	})
}
