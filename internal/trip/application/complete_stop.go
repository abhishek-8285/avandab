package application

import (
	"context"
	"errors"

	"transport-app/internal/shared"
	"transport-app/internal/shared/ports"
	"transport-app/internal/trip/domain"
	"transport-app/internal/trip/domain/aggregate"
)

// CompleteStopCommand contains parameters to complete a stop.
type CompleteStopCommand struct {
	TripID   aggregate.TripID
	StopID   string
	TenantID shared.TenantID
}

// CompleteStopUseCase orchestrates marking a stop as completed after verifying prerequisites.
type CompleteStopUseCase struct {
	uow   ports.UnitOfWork
	clock ports.Clock
}

// NewCompleteStopUseCase creates a new CompleteStopUseCase.
func NewCompleteStopUseCase(uow ports.UnitOfWork, clock ports.Clock) *CompleteStopUseCase {
	return &CompleteStopUseCase{uow: uow, clock: clock}
}

// Execute marks the stop as completed.
func (uc *CompleteStopUseCase) Execute(ctx context.Context, cmd CompleteStopCommand) error {
	return uc.uow.Execute(ctx, func(txCtx ports.TxContext) error {
		repo, ok := txCtx.Repositories().Trips().(domain.TripRepository)
		if !ok {
			return errors.New("failed to retrieve trip repository")
		}
		t, err := repo.Find(txCtx, cmd.TripID, cmd.TenantID)
		if err != nil {
			return err
		}
		if err := t.CompleteStop(cmd.StopID, uc.clock.Now()); err != nil {
			return err
		}
		if err := repo.Save(txCtx, t); err != nil {
			return err
		}
		logAudit(txCtx, ActionUpdate, string(t.ID), nil, nil)
		return nil
	})
}
