package application

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	invoiceDomain "transport-app/internal/invoice/domain"
	invoiceAgg "transport-app/internal/invoice/domain/aggregate"
	"transport-app/internal/payment/domain"
	paymentagg "transport-app/internal/payment/domain/aggregate"
	"transport-app/internal/payment/razorpay"
	"transport-app/internal/shared"
	"transport-app/internal/shared/ports"
)

var (
	// ErrRazorpayNotConfigured signals the server has no Razorpay credentials.
	ErrRazorpayNotConfigured = errors.New("razorpay not configured")
	// ErrRazorpayInvalidSignature signals a payment signature that fails HMAC.
	ErrRazorpayInvalidSignature = errors.New("invalid razorpay signature")
	// ErrInvoiceAlreadySettled signals an invoice with no outstanding balance.
	ErrInvoiceAlreadySettled = errors.New("invoice has no outstanding balance")
	// ErrRazorpayGatewayNotConfigured signals the verify use case has no
	// gateway reader wired; verification fails closed rather than trusting the
	// signature tuple alone.
	ErrRazorpayGatewayNotConfigured = errors.New("razorpay gateway verification not configured")
	// ErrRazorpayPaymentNotFound signals the gateway has no such payment.
	ErrRazorpayPaymentNotFound = errors.New("razorpay payment not found")
	// ErrRazorpayPaymentNotCaptured signals an authorized/failed payment that
	// must not be recorded as collected money.
	ErrRazorpayPaymentNotCaptured = errors.New("razorpay payment not captured")
	// ErrRazorpayOrderMismatch signals the payment belongs to a different order.
	ErrRazorpayOrderMismatch = errors.New("razorpay payment does not belong to the supplied order")
	// ErrRazorpayInvoiceMismatch signals the order was created for a different
	// invoice than the one being credited.
	ErrRazorpayInvoiceMismatch = errors.New("razorpay order does not belong to the supplied invoice")
	// ErrRazorpayAmountMismatch signals the captured amount differs from the
	// order amount (tampered or partial capture).
	ErrRazorpayAmountMismatch = errors.New("razorpay payment amount does not match the order")
)

// CreateRazorpayOrderCommand requests a checkout order for an invoice.
type CreateRazorpayOrderCommand struct {
	TenantID  shared.TenantID
	InvoiceID string
}

// CreateRazorpayOrderResult is the payload handed to Razorpay Checkout.
type CreateRazorpayOrderResult struct {
	OrderID       string `json:"order_id"`
	RazorpayKeyID string `json:"razorpay_key_id"`
	AmountPaise   int64  `json:"amount_paise"`
	Currency      string `json:"currency"`
	InvoiceID     string `json:"invoice_id"`
}

// CreateRazorpayOrderUseCase creates a server-side Razorpay order for the
// invoice's outstanding balance (Spec 11 §5.1).
type CreateRazorpayOrderUseCase struct {
	uow          ports.UnitOfWork
	orderCreator razorpay.OrderCreator
	keyID        string
}

// NewCreateRazorpayOrderUseCase constructs a CreateRazorpayOrderUseCase.
func NewCreateRazorpayOrderUseCase(uow ports.UnitOfWork, orderCreator razorpay.OrderCreator, keyID string) *CreateRazorpayOrderUseCase {
	return &CreateRazorpayOrderUseCase{uow: uow, orderCreator: orderCreator, keyID: keyID}
}

// Execute validates the invoice balance and creates the order server-side.
// The client-supplied amount is never used — the balance is authoritative.
func (uc *CreateRazorpayOrderUseCase) Execute(ctx context.Context, cmd CreateRazorpayOrderCommand) (*CreateRazorpayOrderResult, error) {
	if uc.keyID == "" {
		return nil, ErrRazorpayNotConfigured
	}

	var balance float64
	err := uc.uow.Execute(ctx, func(txCtx ports.TxContext) error {
		invRepo, ok := txCtx.Repositories().Invoices().(invoiceDomain.InvoiceRepository)
		if !ok {
			return errors.New("failed to retrieve invoice repository")
		}
		inv, err := invRepo.Find(txCtx, invoiceAgg.InvoiceID(cmd.InvoiceID), cmd.TenantID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrInvoiceNotFound
			}
			return err
		}
		if inv == nil {
			return ErrInvoiceNotFound
		}
		balance = inv.OutstandingBalance()
		return nil
	})
	if err != nil {
		return nil, err
	}
	if balance <= 0 {
		return nil, ErrInvoiceAlreadySettled
	}

	order, err := uc.orderCreator.CreateOrder(cmd.InvoiceID, balance, "INR")
	if err != nil {
		if errors.Is(err, razorpay.ErrNotConfigured) {
			return nil, ErrRazorpayNotConfigured
		}
		return nil, err
	}

	return &CreateRazorpayOrderResult{
		OrderID:       order.ID,
		RazorpayKeyID: uc.keyID,
		AmountPaise:   order.Amount,
		Currency:      order.Currency,
		InvoiceID:     cmd.InvoiceID,
	}, nil
}

// VerifyRazorpayPaymentCommand carries the Razorpay Checkout callback fields.
type VerifyRazorpayPaymentCommand struct {
	TenantID  shared.TenantID
	InvoiceID string
	OrderID   string
	PaymentID string
	Signature string
}

// VerifyRazorpayPaymentUseCase verifies the payment signature and records the
// payment against the invoice balance (Spec 11 §5.1).
type VerifyRazorpayPaymentUseCase struct {
	uow       ports.UnitOfWork
	recordUC  *RecordPaymentUseCase
	verifier  razorpay.SignatureVerifier
	gateway   razorpay.PaymentGateway
	keySecret string
	clock     ports.Clock
}

// NewVerifyRazorpayPaymentUseCase constructs a VerifyRazorpayPaymentUseCase.
func NewVerifyRazorpayPaymentUseCase(uow ports.UnitOfWork, recordUC *RecordPaymentUseCase, verifier razorpay.SignatureVerifier, keySecret string, clock ports.Clock) *VerifyRazorpayPaymentUseCase {
	return &VerifyRazorpayPaymentUseCase{uow: uow, recordUC: recordUC, verifier: verifier, keySecret: keySecret, clock: clock}
}

// WithPaymentGateway wires the gateway reader used to prove the payment
// belongs to the invoice. Without it verification fails closed.
func (uc *VerifyRazorpayPaymentUseCase) WithPaymentGateway(gw razorpay.PaymentGateway) *VerifyRazorpayPaymentUseCase {
	uc.gateway = gw
	return uc
}

// Execute verifies the HMAC signature AND the gateway-side binding, then
// records the captured amount. The amount recorded is the captured payment
// amount — never the client and never the invoice balance (which may have
// changed since the order was created).
func (uc *VerifyRazorpayPaymentUseCase) Execute(ctx context.Context, cmd VerifyRazorpayPaymentCommand) (paymentagg.PaymentID, error) {
	if uc.keySecret == "" {
		return "", ErrRazorpayNotConfigured
	}
	if !uc.verifier.VerifyPaymentSignature(cmd.OrderID, cmd.PaymentID, cmd.Signature) {
		return "", ErrRazorpayInvalidSignature
	}

	tenantID := cmd.TenantID
	if tenantID == "" {
		tenantID = shared.TenantIDFromContext(ctx)
	}
	if tenantID != "" {
		ctx = shared.ContextWithTenantID(ctx, tenantID)
	}

	existing, err := uc.findRazorpayPayment(ctx, tenantID, cmd.PaymentID)
	if err != nil {
		return "", err
	}
	if existing != "" {
		return existing, nil
	}

	var balance float64
	err = uc.uow.Execute(ctx, func(txCtx ports.TxContext) error {
		invRepo, ok := txCtx.Repositories().Invoices().(invoiceDomain.InvoiceRepository)
		if !ok {
			return errors.New("failed to retrieve invoice repository")
		}
		inv, err := invRepo.Find(txCtx, invoiceAgg.InvoiceID(cmd.InvoiceID), tenantID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrInvoiceNotFound
			}
			return err
		}
		if inv == nil {
			return ErrInvoiceNotFound
		}
		balance = inv.OutstandingBalance()
		return nil
	})
	if err != nil {
		return "", err
	}
	if balance <= 0 {
		return "", ErrInvoiceAlreadySettled
	}

	// Fail closed: signature verification alone is not enough to credit an
	// invoice. Retries of an already-recorded payment returned above, so a
	// fresh verification must prove the binding with the gateway.
	if uc.gateway == nil {
		return "", ErrRazorpayGatewayNotConfigured
	}
	payment, err := uc.gateway.FetchPayment(cmd.PaymentID)
	if err != nil {
		return "", err
	}
	if payment == nil || payment.ID == "" {
		return "", ErrRazorpayPaymentNotFound
	}
	if payment.OrderID != cmd.OrderID {
		return "", ErrRazorpayOrderMismatch
	}
	if !payment.Captured() {
		return "", ErrRazorpayPaymentNotCaptured
	}

	order, err := uc.gateway.FetchOrder(cmd.OrderID)
	if err != nil {
		return "", err
	}
	if order == nil || order.ID == "" {
		return "", ErrRazorpayOrderMismatch
	}
	orderInvoiceID := order.Notes["invoice_id"]
	if orderInvoiceID == "" {
		orderInvoiceID = strings.TrimPrefix(order.Receipt, "rcpt_")
	}
	if orderInvoiceID != cmd.InvoiceID {
		return "", ErrRazorpayInvoiceMismatch
	}
	if payment.Amount <= 0 || order.Amount <= 0 || payment.Amount != order.Amount {
		return "", ErrRazorpayAmountMismatch
	}
	if payment.Currency != "" && order.Currency != "" && payment.Currency != order.Currency {
		return "", ErrRazorpayAmountMismatch
	}

	capturedAmount := float64(payment.Amount) / 100
	reference := cmd.PaymentID
	return uc.recordUC.Execute(ctx, RecordPaymentCommand{
		TenantID:          tenantID,
		InvoiceID:         cmd.InvoiceID,
		PaymentDate:       uc.clock.Now(),
		Amount:            capturedAmount,
		Method:            paymentagg.PaymentMethodRazorpay,
		Reference:         &reference,
		RazorpayOrderID:   cmd.OrderID,
		RazorpayPaymentID: cmd.PaymentID,
		RazorpaySignature: cmd.Signature,
	})
}

func (uc *VerifyRazorpayPaymentUseCase) findRazorpayPayment(ctx context.Context, tenantID shared.TenantID, paymentID string) (paymentagg.PaymentID, error) {
	var found paymentagg.PaymentID
	err := uc.uow.Execute(ctx, func(txCtx ports.TxContext) error {
		payRepo, ok := txCtx.Repositories().Payments().(domain.PaymentRepository)
		if !ok {
			return errors.New("failed to retrieve payment repository")
		}
		id, err := payRepo.ExistsRazorpayPayment(txCtx, tenantID, paymentID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}
		found = id
		return nil
	})
	return found, err
}
