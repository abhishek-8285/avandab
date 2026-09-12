package application

import (
	"context"
	"errors"
	"time"

	"transport-app/internal/booking/domain"
	"transport-app/internal/booking/domain/aggregate"
	"transport-app/internal/shared"
	"transport-app/internal/shared/ports"
)

type UpdateBookingCommand struct {
	BookingID   aggregate.BookingID
	TenantID    shared.TenantID
	CustomerID  string
	RouteID     string
	PickupDate  string
	VehicleType string
	Passengers  int64
	CargoWeight *float64
	Price       float64
	Notes       string
}

type UpdateBookingUseCase struct {
	uow ports.UnitOfWork
}

func NewUpdateBookingUseCase(uow ports.UnitOfWork) *UpdateBookingUseCase {
	return &UpdateBookingUseCase{uow: uow}
}

func (uc *UpdateBookingUseCase) Execute(ctx context.Context, cmd UpdateBookingCommand) error {
	if cmd.CustomerID == "" {
		return errors.New("customer ID is required")
	}
	if cmd.RouteID == "" {
		return errors.New("route ID is required")
	}
	if cmd.VehicleType == "" {
		return errors.New("vehicle type is required")
	}
	if cmd.Passengers < 0 {
		return errors.New("passengers cannot be negative")
	}
	if cmd.Price < 0 {
		return errors.New("price cannot be negative")
	}

	pickupDate, err := time.Parse("2006-01-02", cmd.PickupDate)
	if err != nil {
		pickupDate, err = time.Parse(time.RFC3339, cmd.PickupDate)
		if err != nil {
			pickupDate, err = time.Parse("2006-01-02 15:04", cmd.PickupDate)
			if err != nil {
				pickupDate, err = time.Parse("2006-01-02 15:04:05", cmd.PickupDate)
				if err != nil {
					return errors.New("invalid pickup date format")
				}
			}
		}
	}

	priceMoney := shared.FloatToMoney(cmd.Price, "INR")

	return uc.uow.Execute(ctx, func(txCtx ports.TxContext) error {
		repo, ok := txCtx.Repositories().Bookings().(domain.BookingRepository)
		if !ok {
			return errors.New("failed to retrieve booking repository")
		}

		b, err := repo.Find(txCtx, cmd.BookingID, cmd.TenantID)
		if err != nil {
			return err
		}

		if err := b.Update(
			cmd.CustomerID,
			cmd.RouteID,
			pickupDate,
			cmd.VehicleType,
			cmd.Passengers,
			cmd.CargoWeight,
			priceMoney,
			cmd.Notes,
			time.Now(),
		); err != nil {
			return err
		}

		if err := repo.Save(txCtx, b); err != nil {
			return err
		}
		logAudit(txCtx, ActionUpdate, string(b.ID), nil, nil)
		return nil
	})
}
