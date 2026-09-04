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
)

// CreateBookingCommand defines the parameters required to request a new booking.
type CreateBookingCommand struct {
	TenantID       shared.TenantID
	CustomerID     string
	RouteID        string
	PickupDate     string
	VehicleType    string
	Passengers     int64
	CargoWeight    *float64
	Price          float64
	Notes          string
	IdempotencyKey string
}

// OperationGate decides whether an org may create bookings.
// Implemented by the entitlement service; nil means enforcement off
// (tests, tooling). Missing subscriptions are always allowed so
// bootstrap/legacy tenants are never locked out by billing checks.
type OperationGate interface {
	CanCreateBooking(ctx context.Context, tenantID shared.TenantID) (bool, error)
}

// CreateBookingUseCase orchestrates the validation and persistence of a new booking.
type CreateBookingUseCase struct {
	uow    ports.UnitOfWork
	idGen  ports.IDGenerator
	clock  ports.Clock
	opGate OperationGate
	meter  ports.UsageMeter
}

// NewCreateBookingUseCase creates a new CreateBookingUseCase.
func NewCreateBookingUseCase(uow ports.UnitOfWork, idGen ports.IDGenerator, clock ports.Clock) *CreateBookingUseCase {
	return &CreateBookingUseCase{uow: uow, idGen: idGen, clock: clock}
}

// WithOperationGate attaches commercial enforcement (READ_ONLY/CLOSED orgs
// cannot create bookings). Chain after construction; safe to omit.
func (uc *CreateBookingUseCase) WithOperationGate(gate OperationGate) *CreateBookingUseCase {
	uc.opGate = gate
	return uc
}

// WithUsageMeter attaches quota metering (one monthly-trip unit held per
// booking). Chain after construction; safe to omit.
func (uc *CreateBookingUseCase) WithUsageMeter(meter ports.UsageMeter) *CreateBookingUseCase {
	uc.meter = meter
	return uc
}

// Execute performs the creation of the booking aggregate within transactional boundaries.
func (uc *CreateBookingUseCase) Execute(ctx context.Context, cmd CreateBookingCommand) (aggregate.BookingID, error) {
	if cmd.CustomerID == "" {
		return "", errors.New("customer ID is required")
	}
	if cmd.RouteID == "" {
		return "", errors.New("route ID is required")
	}
	if cmd.VehicleType == "" {
		return "", errors.New("vehicle type is required")
	}
	if cmd.Passengers < 1 {
		return "", errors.New("passengers must be at least 1")
	}
	if cmd.Price < 0 {
		return "", errors.New("price cannot be negative")
	}
	if uc.opGate != nil {
		ok, err := uc.opGate.CanCreateBooking(ctx, cmd.TenantID)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", errors.New("booking creation blocked for this organisation (subscription status)")
		}
	}

	pickupDate, err := time.Parse("2006-01-02", cmd.PickupDate)
	if err != nil {
		pickupDate, err = time.Parse(time.RFC3339, cmd.PickupDate)
		if err != nil {
			pickupDate, err = time.Parse("2006-01-02 15:04", cmd.PickupDate)
			if err != nil {
				pickupDate, err = time.Parse("2006-01-02 15:04:05", cmd.PickupDate)
				if err != nil {
					return "", errors.New("invalid pickup date format")
				}
			}
		}
	}

	bookingID := aggregate.BookingID(uc.idGen.GenerateUUID())
	bookingNumber := uc.idGen.GenerateDisplayID("BK")
	priceMoney := shared.FloatToMoney(cmd.Price, "INR")

	booking := aggregate.NewBookingAggregate(
		bookingID,
		cmd.TenantID,
		bookingNumber,
		cmd.CustomerID,
		cmd.RouteID,
		pickupDate,
		cmd.VehicleType,
		cmd.Passengers,
		cmd.CargoWeight,
		priceMoney,
		cmd.Notes,
		uc.clock.Now(),
	)

	booking.SetIdempotencyKey(cmd.IdempotencyKey)

	var resultID aggregate.BookingID
	err = uc.uow.Execute(ctx, func(txCtx ports.TxContext) error {
		repo, ok := txCtx.Repositories().Bookings().(domain.BookingRepository)
		if !ok {
			return errors.New("failed to retrieve booking repository")
		}
		// Idempotent replay: same key returns the original booking, no new row.
		if cmd.IdempotencyKey != "" {
			if existing, err := repo.FindByIdempotencyKey(txCtx, cmd.IdempotencyKey, cmd.TenantID); err == nil && existing != nil {
				resultID = existing.ID
				return nil
			}
		}
		if err := repo.Save(txCtx, booking); err != nil {
			// Lost-race replay: a concurrent insert with the same key won.
			if cmd.IdempotencyKey != "" && strings.Contains(err.Error(), "UNIQUE constraint failed") {
				if existing, rerr := repo.FindByIdempotencyKey(txCtx, cmd.IdempotencyKey, cmd.TenantID); rerr == nil && existing != nil {
					resultID = existing.ID
					return nil
				}
			}
			return err
		}
		resultID = booking.ID
		// Hold one monthly-trip quota unit in the same transaction: a full
		// plan rejects the booking instead of silently over-serving.
		if uc.meter != nil {
			if err := uc.meter.ReserveBooking(txCtx, repository.TxFromContext(txCtx), cmd.TenantID, string(booking.ID)); err != nil {
				return err
			}
		}
		logAudit(txCtx, ActionCreate, string(booking.ID), nil, nil)
		return nil
	})

	if err != nil {
		return "", err
	}

	return resultID, nil
}
