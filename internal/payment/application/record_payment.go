package application

import (
	"context"
	"database/sql"
	"errors"
	"time"

	invoiceDomain "transport-app/internal/invoice/domain"
	invoiceAgg "transport-app/internal/invoice/domain/aggregate"
	"transport-app/internal/payment/domain"
	paymentagg "transport-app/internal/payment/domain/aggregate"
	"transport-app/internal/shared"
	"transport-app/internal/shared/ports"
)

var ErrInvoiceNotFound = errors.New("invoice not found")
var ErrPaymentExceedsBalance = errors.New("payment exceeds invoice outstanding balance")

// RecordPaymentCommand contains parameters to register a payment.
type RecordPaymentCommand struct {
	TenantID    shared.TenantID
	InvoiceID   string
	PaymentDate time.Time
	Amount      float64
	Method      paymentagg.PaymentMethod
	Reference   *string
	Remarks     *string

	// RazorpayOrderID, RazorpayPaymentID, RazorpaySignature are persisted to
	// the payment row when present (Spec 11 §5.1 /verify flow).
	RazorpayOrderID   string
	RazorpayPaymentID string
	RazorpaySignature string
}

// RecordPaymentUseCase records a payment.
type RecordPaymentUseCase struct {
	uow   ports.UnitOfWork
	idGen ports.IDGenerator
	clock ports.Clock
}

// NewRecordPaymentUseCase constructs a new RecordPaymentUseCase.
func NewRecordPaymentUseCase(uow ports.UnitOfWork, idGen ports.IDGenerator, clock ports.Clock) *RecordPaymentUseCase {
	return &RecordPaymentUseCase{uow: uow, idGen: idGen, clock: clock}
}

// Execute performs validation, creates the aggregate, and saves it.
func (uc *RecordPaymentUseCase) Execute(ctx context.Context, cmd RecordPaymentCommand) (paymentagg.PaymentID, error) {
	if cmd.InvoiceID == "" {
		return "", errors.New("invoice ID is required")
	}
	if cmd.Amount <= 0 {
		return "", errors.New("payment amount must be greater than zero")
	}

	var id paymentagg.PaymentID

	err := uc.uow.Execute(ctx, func(txCtx ports.TxContext) error {
		invoiceRepo, ok := txCtx.Repositories().Invoices().(invoiceDomain.InvoiceRepository)
		if !ok {
			return errors.New("failed to retrieve invoice repository")
		}

		payRepo, ok := txCtx.Repositories().Payments().(domain.PaymentRepository)
		if !ok {
			return errors.New("failed to retrieve payment repository")
		}

		inv, err := invoiceRepo.Find(txCtx, invoiceAgg.InvoiceID(cmd.InvoiceID), cmd.TenantID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrInvoiceNotFound
			}
			return err
		}
		if inv == nil {
			return ErrInvoiceNotFound
		}

		payment := paymentagg.NewPaymentAggregate(
			paymentagg.PaymentID(uc.idGen.GenerateUUID()),
			cmd.TenantID,
			cmd.InvoiceID,
			cmd.PaymentDate,
			cmd.Amount,
			cmd.Method,
			cmd.Reference,
			cmd.Remarks,
			uc.clock.Now(),
		)

		// Claim the idempotency key BEFORE touching the invoice: a replayed
		// request must return the existing payment untouched, never apply
		// the amount twice. SaveIfNew reports whether this call won the row.
		created, err := payRepo.SaveIfNew(txCtx, payment)
		if err != nil {
			return err
		}
		if !created {
			id = payment.ID
			return nil
		}

		if err := inv.ApplyPayment(cmd.Amount, uc.clock.Now()); err != nil {
			return err
		}

		if err := invoiceRepo.Save(txCtx, inv); err != nil {
			return err
		}

		if cmd.RazorpayPaymentID != "" {
			if err := payRepo.SetRazorpayFields(txCtx, payment.ID, cmd.TenantID, cmd.RazorpayOrderID, cmd.RazorpayPaymentID, cmd.RazorpaySignature); err != nil {
				return err
			}
		}
		id = payment.ID
		return nil
	})

	if err != nil {
		switch err.Error() {
		case ErrInvoiceNotFound.Error():
			return "", ErrInvoiceNotFound
		case ErrPaymentExceedsBalance.Error():
			return "", ErrPaymentExceedsBalance
		default:
			return "", err
		}
	}

	return id, nil
}
