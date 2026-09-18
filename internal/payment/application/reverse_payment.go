package application

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	invoiceDomain "transport-app/internal/invoice/domain"
	invoiceAgg "transport-app/internal/invoice/domain/aggregate"
	"transport-app/internal/payment/domain"
	paymentagg "transport-app/internal/payment/domain/aggregate"
	"transport-app/internal/shared"
	"transport-app/internal/shared/ports"
)

var (
	ErrPaymentNotFound        = errors.New("payment not found")
	ErrMissingOriginalPayment = errors.New("original payment ID is required")
	ErrMissingReversalReason  = errors.New("reversal reason is required")
)

type ReversePaymentCommand struct {
	TenantID      shared.TenantID
	OriginalPayID paymentagg.PaymentID
	Reason        string
}

type ReversePaymentUseCase struct {
	uow   ports.UnitOfWork
	idGen ports.IDGenerator
	clock ports.Clock
}

func NewReversePaymentUseCase(uow ports.UnitOfWork, idGen ports.IDGenerator, clock ports.Clock) *ReversePaymentUseCase {
	return &ReversePaymentUseCase{uow: uow, idGen: idGen, clock: clock}
}

func (uc *ReversePaymentUseCase) Execute(ctx context.Context, cmd ReversePaymentCommand) (paymentagg.PaymentID, error) {
	if cmd.OriginalPayID == "" {
		return "", ErrMissingOriginalPayment
	}
	if cmd.Reason == "" {
		return "", ErrMissingReversalReason
	}

	var id paymentagg.PaymentID

	err := uc.uow.Execute(ctx, func(txCtx ports.TxContext) error {
		payRepo, ok := txCtx.Repositories().Payments().(domain.PaymentRepository)
		if !ok {
			return errors.New("failed to retrieve payment repository")
		}

		original, err := payRepo.Find(txCtx, cmd.OriginalPayID, cmd.TenantID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrPaymentNotFound
			}
			return err
		}
		if original == nil {
			return ErrPaymentNotFound
		}

		reversalRef := fmt.Sprintf("REVERSAL:%s", string(original.ID))

		// Idempotency guard: if a reversal for this payment already exists,
		// return its ID without touching the invoice again — a retried
		// refund must not double-decrement paid_amount.
		existingReversalID, err := payRepo.FindByReference(txCtx, reversalRef, cmd.TenantID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if existingReversalID != "" {
			id = existingReversalID
			return nil
		}

		reversal := paymentagg.NewPaymentAggregate(
			paymentagg.PaymentID(uc.idGen.GenerateUUID()),
			cmd.TenantID,
			original.InvoiceID,
			uc.clock.Now(),
			-original.Amount,
			original.Method,
			&reversalRef,
			&cmd.Reason,
			uc.clock.Now(),
		)

		// Claim the reversal row BEFORE touching the invoice, mirroring
		// RecordPayment: a retried refund must not decrement paid_amount twice.
		created, err := payRepo.SaveIfNew(txCtx, reversal)
		if err != nil {
			return err
		}
		if !created {
			id = reversal.ID
			return nil
		}

		invoiceRepo, ok := txCtx.Repositories().Invoices().(invoiceDomain.InvoiceRepository)
		if !ok {
			return errors.New("failed to retrieve invoice repository")
		}

		inv, err := invoiceRepo.Find(txCtx, invoiceAgg.InvoiceID(original.InvoiceID), cmd.TenantID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrInvoiceNotFound
			}
			return err
		}
		if inv == nil {
			return ErrInvoiceNotFound
		}

		if err := inv.ApplyPayment(-original.Amount, uc.clock.Now()); err != nil {
			return err
		}

		if err := invoiceRepo.Save(txCtx, inv); err != nil {
			return err
		}

		id = reversal.ID
		return nil
	})

	if err != nil {
		switch err.Error() {
		case ErrPaymentNotFound.Error():
			return "", ErrPaymentNotFound
		case ErrInvoiceNotFound.Error():
			return "", ErrInvoiceNotFound
		default:
			return "", err
		}
	}

	return id, nil
}
