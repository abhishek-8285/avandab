package application

import (
	"context"
	"errors"

	"transport-app/internal/repository"
	"transport-app/internal/shared"
	"transport-app/internal/shared/ports"
	"transport-app/internal/trip/domain"
	"transport-app/internal/trip/domain/aggregate"
)

// CompleteTripCommand contains parameters to transition a trip to completed.
type CompleteTripCommand struct {
	TripID   aggregate.TripID
	TenantID shared.TenantID
	// OnCompleted runs inside the same UnitOfWork transaction after the trip
	// is saved, letting callers attach detentions/invoices atomically with
	// the completion (Spec 02 §6 — no torn states).
	OnCompleted func(txCtx ports.TxContext, trip *aggregate.TripAggregate) error
}

// CompleteTripUseCase orchestrates completing a trip.
type CompleteTripUseCase struct {
	uow   ports.UnitOfWork
	clock ports.Clock
	meter ports.UsageMeter
}

// NewCompleteTripUseCase creates a new CompleteTripUseCase.
func NewCompleteTripUseCase(uow ports.UnitOfWork, clock ports.Clock) *CompleteTripUseCase {
	return &CompleteTripUseCase{uow: uow, clock: clock}
}

// WithUsageMeter converts the booking's quota hold into usage on completion.
// Chain after construction; safe to omit.
func (uc *CompleteTripUseCase) WithUsageMeter(meter ports.UsageMeter) *CompleteTripUseCase {
	uc.meter = meter
	return uc
}

// Execute performs the transition.
func (uc *CompleteTripUseCase) Execute(ctx context.Context, cmd CompleteTripCommand) error {
	return uc.uow.Execute(ctx, func(txCtx ports.TxContext) error {
		repo, ok := txCtx.Repositories().Trips().(domain.TripRepository)
		if !ok {
			return errors.New("failed to retrieve trip repository")
		}
		t, err := repo.Find(txCtx, cmd.TripID, cmd.TenantID)
		if err != nil {
			return err
		}
		if err := t.Complete(uc.clock.Now()); err != nil {
			return err
		}
		if err := repo.Save(txCtx, t); err != nil {
			return err
		}
		// Convert the booking's quota hold into usage. Metering is keyed by
		// booking so retried completions cannot double-count.
		if uc.meter != nil && t.BookingID != nil && *t.BookingID != "" {
			if err := uc.meter.CommitBooking(txCtx, repository.TxFromContext(txCtx), cmd.TenantID, *t.BookingID); err != nil {
				return err
			}
		}
		logAudit(txCtx, ActionComplete, string(t.ID), nil, nil)
		if cmd.OnCompleted != nil {
			return cmd.OnCompleted(txCtx, t)
		}
		return nil
	})
}
