package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	bookingapp "transport-app/internal/booking/application"
	bookingagg "transport-app/internal/booking/domain/aggregate"
	invoiceapp "transport-app/internal/invoice/application"
	"transport-app/internal/shared"
	"transport-app/internal/shared/ports"
	tripapp "transport-app/internal/trip/application"
	tripagg "transport-app/internal/trip/domain/aggregate"
)

// ConfirmBookingAndCreateTrip coordinates the booking-to-operations handoff.
// Existing use cases own their transactions; compensation prevents a confirmed
// booking from being left without a trip when trip creation fails.
type ConfirmBookingAndCreateTrip struct {
	confirm *bookingapp.ConfirmBookingUseCase
	cancel  *bookingapp.CancelBookingUseCase
	create  *tripapp.CreateTripUseCase
	invoice *invoiceapp.GenerateInvoiceUseCase
	uow     ports.UnitOfWork
}

func NewConfirmBookingAndCreateTrip(uow ports.UnitOfWork, confirm *bookingapp.ConfirmBookingUseCase, cancel *bookingapp.CancelBookingUseCase, create *tripapp.CreateTripUseCase, invoice *invoiceapp.GenerateInvoiceUseCase) *ConfirmBookingAndCreateTrip {
	return &ConfirmBookingAndCreateTrip{uow: uow, confirm: confirm, cancel: cancel, create: create, invoice: invoice}
}

type ConfirmBookingAndCreateTripCommand struct {
	BookingID      bookingagg.BookingID
	TenantID       shared.TenantID
	RouteID        string
	DepartureTime  time.Time
	IdempotencyKey string
	CustomerID     string
}

func (uc *ConfirmBookingAndCreateTrip) Execute(ctx context.Context, cmd ConfirmBookingAndCreateTripCommand) (tripagg.TripID, error) {
	if uc == nil || uc.uow == nil || uc.confirm == nil || uc.cancel == nil || uc.create == nil || uc.invoice == nil {
		return "", errors.New("booking trip workflow is not configured")
	}
	if cmd.TenantID == "" || cmd.BookingID == "" || cmd.RouteID == "" || cmd.DepartureTime.IsZero() {
		return "", errors.New("booking trip workflow requires tenant, booking, route, and departure time")
	}
	var id tripagg.TripID
	err := uc.uow.Execute(ctx, func(txCtx ports.TxContext) error {
		if err := uc.confirm.Execute(txCtx, bookingapp.ConfirmBookingCommand{BookingID: cmd.BookingID, TenantID: cmd.TenantID}); err != nil {
			return err
		}
		bookingID := string(cmd.BookingID)
		created, err := uc.create.Execute(txCtx, tripapp.CreateTripCommand{TenantID: cmd.TenantID, BookingID: &bookingID, RouteID: cmd.RouteID, DepartureTime: cmd.DepartureTime, IdempotencyKey: cmd.IdempotencyKey})
		if err == nil {
			id = created
			tripID := string(id)
			_, _, err = uc.invoice.GenerateInTx(txCtx, invoiceapp.GenerateInvoiceCommand{TenantID: cmd.TenantID, BookingID: bookingID, CustomerID: cmd.CustomerID, TripID: &tripID})
			if err != nil {
				return fmt.Errorf("create invoice: %w", err)
			}
			return nil
		}
		if strings.Contains(err.Error(), "already has active trip") {
			_, _, invoiceErr := uc.invoice.GenerateInTx(txCtx, invoiceapp.GenerateInvoiceCommand{
				TenantID: cmd.TenantID, BookingID: bookingID, CustomerID: cmd.CustomerID,
			})
			if invoiceErr != nil {
				return fmt.Errorf("create invoice for existing trip: %w", invoiceErr)
			}
			return nil
		}
		if cancelErr := uc.cancel.Execute(txCtx, bookingapp.CancelBookingCommand{BookingID: cmd.BookingID, TenantID: cmd.TenantID}); cancelErr != nil {
			return fmt.Errorf("create trip: %w; compensate booking: %v", err, cancelErr)
		}
		return fmt.Errorf("create trip: %w; booking compensation applied", err)
	})
	return id, err
}
