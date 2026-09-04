package application

import (
	"context"
	"errors"
	"strings"
	"time"
	"transport-app/internal/booking/domain"
	"transport-app/internal/booking/domain/aggregate"
	"transport-app/internal/repository"
	"transport-app/internal/shared"
	"transport-app/internal/shared/ports"
	tripdomain "transport-app/internal/trip/domain"
	tripaggregate "transport-app/internal/trip/domain/aggregate"
)

// CancelBookingCommand defines parameters for cancelling a booking.
type CancelBookingCommand struct {
	BookingID aggregate.BookingID
	TenantID  shared.TenantID
}

// CancelBookingUseCase orchestrates the cancellation of a booking.
type CancelBookingUseCase struct {
	uow   ports.UnitOfWork
	clock ports.Clock
	meter ports.UsageMeter
}

// NewCancelBookingUseCase creates a new CancelBookingUseCase.
func NewCancelBookingUseCase(uow ports.UnitOfWork, clock ports.Clock) *CancelBookingUseCase {
	return &CancelBookingUseCase{uow: uow, clock: clock}
}

// WithUsageMeter returns quota holds on cancel. Chain after construction;
// safe to omit.
func (uc *CancelBookingUseCase) WithUsageMeter(meter ports.UsageMeter) *CancelBookingUseCase {
	uc.meter = meter
	return uc
}

// Execute marks the booking aggregate as cancelled within transactional boundaries.
// Saga: also cancels the linked trip (same tenant, same transaction) when it is
// non-terminal. A missing or already-terminal trip never fails the booking cancel.
func (uc *CancelBookingUseCase) Execute(ctx context.Context, cmd CancelBookingCommand) error {
	return uc.uow.Execute(ctx, func(txCtx ports.TxContext) error {
		repo, ok := txCtx.Repositories().Bookings().(domain.BookingRepository)
		if !ok {
			return errors.New("failed to retrieve booking repository")
		}

		booking, err := repo.Find(txCtx, cmd.BookingID, cmd.TenantID)
		if err != nil {
			return err
		}

		now := uc.clock.Now()
		wasActive := booking.Status != aggregate.BookingCancelled && booking.Status != aggregate.BookingCompleted
		if err := booking.Cancel(now); err != nil {
			return err
		}

		if err := repo.Save(txCtx, booking); err != nil {
			return err
		}
		if err := uc.cancelLinkedTrip(txCtx, string(cmd.BookingID), cmd.TenantID, now); err != nil {
			return err
		}
		// Return the quota hold, but only for a real transition: re-cancelling
		// must not credit the meter twice.
		if wasActive && uc.meter != nil {
			if err := uc.meter.ReleaseBooking(txCtx, repository.TxFromContext(txCtx), cmd.TenantID, string(cmd.BookingID)); err != nil {
				return err
			}
		}
		logAudit(txCtx, ActionCancel, string(booking.ID), nil, nil)
		return nil
	})
}

// cancelLinkedTrip cancels the trip linked to bookingID in the same transaction.
// Returns nil when no trip exists or the trip is already terminal
// (completed/cancelled). Completed trips cannot be cancelled per the trip
// aggregate, so they are left untouched.
func (uc *CancelBookingUseCase) cancelLinkedTrip(txCtx ports.TxContext, bookingID string, tenantID shared.TenantID, now time.Time) error {
	tripRepo, ok := txCtx.Repositories().Trips().(tripdomain.TripRepository)
	if !ok {
		return errors.New("failed to retrieve trip repository")
	}
	trip, err := tripRepo.FindByBookingID(txCtx, bookingID, tenantID)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			return nil
		}
		return err
	}
	if trip == nil {
		return nil
	}
	switch trip.Status {
	case tripaggregate.TripCompleted, tripaggregate.TripCancelled:
		return nil
	}
	if err := trip.Cancel(now); err != nil {
		// Defensive: a terminal-trip race must not fail the booking cancel.
		if strings.Contains(strings.ToLower(err.Error()), "completed") {
			return nil
		}
		return err
	}
	return tripRepo.Save(txCtx, trip)
}
