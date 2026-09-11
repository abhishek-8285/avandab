package application

import (
	"context"
	"errors"
	"time"

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
	// CloseOdometer is the SOP trip-close odometer reading (ZMOTM_MMS p.8).
	// Nil = closed without a reading (reading triple = CloseOdometer +
	// CompletedAt). Breakdown alerts are filed by callers, not here.
	CloseOdometer *float64
	// ClosedAt is the driver-reported close date/time (ZMOTM_MMS pp.6-8 close
	// dialog; offline closes sync late). Nil = server now. Future values are
	// rejected — a close cannot happen after it is recorded.
	ClosedAt *time.Time
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
		now := uc.clock.Now()
		if cmd.ClosedAt != nil {
			if cmd.ClosedAt.After(now.Add(time.Minute)) {
				return errors.New("closed_at cannot be in the future")
			}
			now = *cmd.ClosedAt
		}
		if err := t.Complete(now); err != nil {
			return err
		}
		if cmd.CloseOdometer != nil {
			if err := t.RecordCloseReading(*cmd.CloseOdometer, now); err != nil {
				return err
			}
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
