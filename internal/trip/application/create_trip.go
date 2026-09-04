package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	bookingDomain "transport-app/internal/booking/domain"
	bookingAggregate "transport-app/internal/booking/domain/aggregate"
	"transport-app/internal/shared"
	"transport-app/internal/shared/ports"
	"transport-app/internal/trip/domain"
	"transport-app/internal/trip/domain/aggregate"
)

type CreateTripCommand struct {
	TenantID       shared.TenantID
	BookingID      *string
	RouteID        string
	DepartureTime  time.Time
	Remarks        string
	IdempotencyKey string
}

type CreateTripUseCase struct {
	uow   ports.UnitOfWork
	idGen ports.IDGenerator
	clock ports.Clock
}

func NewCreateTripUseCase(uow ports.UnitOfWork, idGen ports.IDGenerator, clock ports.Clock) *CreateTripUseCase {
	return &CreateTripUseCase{uow: uow, idGen: idGen, clock: clock}
}

func (uc *CreateTripUseCase) Execute(ctx context.Context, cmd CreateTripCommand) (aggregate.TripID, error) {
	if cmd.RouteID == "" {
		return "", errors.New("route ID is required")
	}
	if cmd.DepartureTime.IsZero() {
		return "", errors.New("departure time is required")
	}

	tripID := aggregate.TripID(uc.idGen.GenerateUUID())
	tripNumber := uc.idGen.GenerateDisplayID("TR")

	trip := aggregate.NewTripAggregate(
		tripID,
		cmd.TenantID,
		tripNumber,
		cmd.BookingID,
		cmd.RouteID,
		cmd.DepartureTime,
		cmd.Remarks,
		uc.clock.Now(),
	)
	trip.SetIdempotencyKey(cmd.IdempotencyKey)

	var resultID aggregate.TripID
	err := uc.uow.Execute(ctx, func(txCtx ports.TxContext) error {
		repo, ok := txCtx.Repositories().Trips().(domain.TripRepository)
		if !ok {
			return errors.New("failed to retrieve trip repository")
		}
		// Idempotent replay: same key returns the original trip, no new row.
		if cmd.IdempotencyKey != "" {
			if existing, err := repo.FindByIdempotencyKey(txCtx, cmd.IdempotencyKey, cmd.TenantID); err == nil && existing != nil {
				resultID = existing.ID
				return nil
			}
		}
		// If a booking is linked, it must exist in the same tenant, be in a
		// trippable state, and not already have an active trip. Prevents
		// orphan trips and duplicate trips per booking.
		if cmd.BookingID != nil && *cmd.BookingID != "" {
			bookingRepo, ok := txCtx.Repositories().Bookings().(bookingDomain.BookingRepository)
			if !ok {
				return errors.New("failed to retrieve booking repository")
			}
			booking, err := bookingRepo.Find(txCtx, bookingAggregate.BookingID(*cmd.BookingID), cmd.TenantID)
			if err != nil {
				return fmt.Errorf("booking %s not found: %w", *cmd.BookingID, err)
			}
			if string(booking.TenantID) != string(cmd.TenantID) {
				return errors.New("booking belongs to a different tenant")
			}
			switch booking.Status {
			case bookingAggregate.BookingPending, bookingAggregate.BookingConfirmed:
				// allowed
			default:
				return fmt.Errorf("booking %s is %s, cannot create trip", *cmd.BookingID, booking.Status)
			}
			if existing, err := repo.FindByBookingID(txCtx, *cmd.BookingID, cmd.TenantID); err == nil && existing != nil {
				switch existing.Status {
				case aggregate.TripCompleted, aggregate.TripCancelled:
					// terminal — a replacement trip is allowed
				default:
					return fmt.Errorf("booking %s already has active trip %s", *cmd.BookingID, existing.TripNumber)
				}
			}
		}
		if err := repo.Save(txCtx, trip); err != nil {
			// Lost-race replay: a concurrent insert with the same key won.
			// Return the winner instead of a duplicate error.
			if cmd.IdempotencyKey != "" && strings.Contains(err.Error(), "UNIQUE constraint failed") {
				if existing, rerr := repo.FindByIdempotencyKey(txCtx, cmd.IdempotencyKey, cmd.TenantID); rerr == nil && existing != nil {
					resultID = existing.ID
					return nil
				}
			}
			return err
		}
		resultID = trip.ID
		logAudit(txCtx, ActionCreate, string(trip.ID), nil, nil)
		return nil
	})

	if err != nil {
		return "", err
	}

	return resultID, nil
}
