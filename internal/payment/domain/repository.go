package domain

import (
	"context"
	"time"

	"transport-app/internal/payment/domain/aggregate"
	"transport-app/internal/shared"
)

// PaymentReadModel optimized for read queries.
type PaymentReadModel struct {
	ID            string
	InvoiceID     string
	InvoiceNumber string
	PaymentDate   time.Time
	Amount        float64
	Method        string
	Reference     *string
	Remarks       *string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// PaymentRepository defines the persistence contract for payments.
type PaymentRepository interface {
	Save(ctx context.Context, p *aggregate.PaymentAggregate) error
	// SaveIfNew atomically claims the payment's idempotency key inside the
	// caller's transaction. It returns created=false with p.ID rewritten to
	// the existing row when the key was already claimed, so use cases can
	// return the prior payment WITHOUT repeating invoice effects. Callers
	// must gate all balance changes on created=true.
	SaveIfNew(ctx context.Context, p *aggregate.PaymentAggregate) (created bool, err error)
	Find(ctx context.Context, id aggregate.PaymentID, tenantID shared.TenantID) (*aggregate.PaymentAggregate, error)
	FindByReference(ctx context.Context, reference string, tenantID shared.TenantID) (aggregate.PaymentID, error)
	GetReadModel(ctx context.Context, id aggregate.PaymentID, tenantID shared.TenantID) (PaymentReadModel, error)
	GetPaymentsByInvoice(ctx context.Context, invoiceID string, tenantID shared.TenantID) ([]PaymentReadModel, error)
	SearchReadModels(ctx context.Context, tenantID shared.TenantID, method string, limit int, offset int) ([]PaymentReadModel, int64, error)

	// SetRazorpayFields stores the Razorpay order/payment/signature identifiers
	// on an existing payment row. Used by the /verify flow (Spec 11 §5.1).
	SetRazorpayFields(ctx context.Context, id aggregate.PaymentID, tenantID shared.TenantID, orderID, paymentID, signature string) error

	// ExistsRazorpayPayment returns the payment ID already recorded against a
	// Razorpay payment ID, or sql.ErrNoRows when none exists. Arbiter for the
	// /verify-vs-webhook race (UNIQUE partial index idx_payments_razorpay_payment).
	ExistsRazorpayPayment(ctx context.Context, tenantID shared.TenantID, paymentID string) (aggregate.PaymentID, error)

	// ExistsWebhookEvent returns the payment ID already processed for a Razorpay
	// webhook event ID, or sql.ErrNoRows when none exists. Restart-safe
	// idempotency (UNIQUE partial index idx_payments_webhook_event).
	ExistsWebhookEvent(ctx context.Context, tenantID shared.TenantID, eventID string) (aggregate.PaymentID, error)

	// SetWebhookEventID persists the Razorpay webhook event ID on a payment row
	// so duplicate webhook deliveries are deduplicated across restarts.
	SetWebhookEventID(ctx context.Context, id aggregate.PaymentID, tenantID shared.TenantID, eventID string) error
}
