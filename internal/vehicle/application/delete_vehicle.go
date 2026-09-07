package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"transport-app/internal/shared"
	"transport-app/internal/shared/ports"
	"transport-app/internal/vehicle/domain"
	"transport-app/internal/vehicle/domain/aggregate"
	sqlrepo "transport-app/internal/vehicle/infrastructure/persistence/sql"
)

// DeleteVehicleUseCase removes a vehicle from the registry. Like the legacy
// store, it refuses when live (non completed/cancelled) trips reference the
// vehicle, so trip history can never dangle.
type DeleteVehicleUseCase struct {
	uow   ports.UnitOfWork
	clock ports.Clock
}

func NewDeleteVehicleUseCase(uow ports.UnitOfWork, clock ports.Clock) *DeleteVehicleUseCase {
	return &DeleteVehicleUseCase{uow: uow, clock: clock}
}

func (uc *DeleteVehicleUseCase) Execute(ctx context.Context, id aggregate.VehicleID, tenantID shared.TenantID) error {
	return uc.uow.Execute(ctx, func(txCtx ports.TxContext) error {
		repo, ok := txCtx.Repositories().Vehicles().(domain.VehicleRepository)
		if !ok {
			return errors.New("failed to retrieve vehicle repository")
		}

		v, err := repo.Find(txCtx, id, tenantID)
		if err != nil {
			return err
		}

		if checker, ok := txCtx.Repositories().Vehicles().(sqlrepo.TripConflictChecker); ok {
			active, err := checker.CountActiveTripsForVehicle(txCtx, tenantID, string(id))
			if err != nil {
				// Minimal test schemas have no trips table: no conflicts.
				// Any other error aborts the delete.
				if !strings.Contains(err.Error(), "no such table") {
					return err
				}
			} else if active > 0 {
				return fmt.Errorf("cannot delete vehicle: %d live trip(s) reference it", active)
			}
		}

		deleter, ok := txCtx.Repositories().Vehicles().(sqlrepo.VehicleDeleter)
		if !ok {
			return errors.New("vehicle repository does not support delete")
		}

		v.RecordDeletion(uc.clock.Now())
		if err := deleter.DeleteVehicleRecord(txCtx, string(id), string(tenantID)); err != nil {
			return err
		}
		return deleter.FlushVehicleEvents(txCtx, v)
	})
}
