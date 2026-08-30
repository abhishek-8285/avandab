package application

import (
	"context"
	"errors"

	"transport-app/internal/shared"
	"transport-app/internal/shared/ports"
	"transport-app/internal/trip/domain"
	"transport-app/internal/trip/domain/aggregate"
)

// ReachStopCommand contains parameters to transition a trip stop to arrived.
type ReachStopCommand struct {
	TripID   aggregate.TripID
	StopID   string
	TenantID shared.TenantID
}

// ReachStopUseCase orchestrates marking arrival at a specific multi-stop leg.
type ReachStopUseCase struct {
	uow   ports.UnitOfWork
	clock ports.Clock
}

// NewReachStopUseCase creates a new ReachStopUseCase.
func NewReachStopUseCase(uow ports.UnitOfWork, clock ports.Clock) *ReachStopUseCase {
	return &ReachStopUseCase{uow: uow, clock: clock}
}

// Execute performs arrival transition on the stop.
func (uc *ReachStopUseCase) Execute(ctx context.Context, cmd ReachStopCommand) error {
	return uc.uow.Execute(ctx, func(txCtx ports.TxContext) error {
		repo, ok := txCtx.Repositories().Trips().(domain.TripRepository)
		if !ok {
			return errors.New("failed to retrieve trip repository")
		}
		t, err := repo.Find(txCtx, cmd.TripID, cmd.TenantID)
		if err != nil {
			return err
		}
		if err := t.ReachStop(cmd.StopID, uc.clock.Now()); err != nil {
			return err
		}
		if err := repo.Save(txCtx, t); err != nil {
			return err
		}
		logAudit(txCtx, ActionUpdate, string(t.ID), nil, nil)
		return nil
	})
}
