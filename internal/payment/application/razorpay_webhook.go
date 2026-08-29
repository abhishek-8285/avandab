package application

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"transport-app/internal/events"
	"transport-app/internal/payment/domain"
	paymentagg "transport-app/internal/payment/domain/aggregate"
	"transport-app/internal/shared"
	"transport-app/internal/shared/ports"
)

const webhookEventTTL = 24 * time.Hour

var (
	ErrWebhookNotConfigured           = errors.New("razorpay webhook secret not configured")
	ErrWebhookInvalidSignature        = errors.New("invalid razorpay webhook signature")
	ErrWebhookInvoiceMissing          = errors.New("razorpay webhook payload missing invoice_id")
	ErrWebhookOriginalPaymentNotFound = errors.New("razorpay webhook refund original payment not found")
)

// RazorpayPaymentEntity captures the shared payment fields from Razorpay payloads.
type RazorpayPaymentEntity struct {
	ID               string `json:"id"`
	OrderID          string `json:"order_id"`
	Amount           int64  `json:"amount"`
	Currency         string `json:"currency"`
	Status           string `json:"status"`
	ErrorCode        string `json:"error_code"`
	ErrorDescription string `json:"error_description"`
	Notes            struct {
		InvoiceID string `json:"invoice_id"`
	} `json:"notes"`
}

// RazorpayOrderEntity captures the order fields from an order.paid payload.
type RazorpayOrderEntity struct {
	ID         string `json:"id"`
	AmountPaid int64  `json:"amount_paid"`
	Currency   string `json:"currency"`
	Status     string `json:"status"`
	Notes      struct {
		InvoiceID string `json:"invoice_id"`
	} `json:"notes"`
}

// RazorpayRefundEntity captures the refund fields from a refund.processed payload.
type RazorpayRefundEntity struct {
	ID        string `json:"id"`
	PaymentID string `json:"payment_id"`
	Amount    int64  `json:"amount"`
	Currency  string `json:"currency"`
	Status    string `json:"status"`
	Notes     struct {
		InvoiceID string `json:"invoice_id"`
	} `json:"notes"`
}

// RazorpayWebhookEvent mirrors the Razorpay webhook payload for payment events.
type RazorpayWebhookEvent struct {
	Event   string `json:"event"`
	Payload struct {
		Payment struct {
			Entity RazorpayPaymentEntity `json:"entity"`
		} `json:"payment"`
		Order struct {
			Entity RazorpayOrderEntity `json:"entity"`
		} `json:"order"`
		Refund struct {
			Entity RazorpayRefundEntity `json:"entity"`
		} `json:"refund"`
	} `json:"payload"`
}

// RazorpayPaymentFailedEvent is emitted when Razorpay reports a failed payment.
type RazorpayPaymentFailedEvent struct {
	RazorpayEventID   string
	RazorpayPaymentID string
	OrderID           string
	InvoiceID         string
	Amount            float64
	Currency          string
	ErrorCode         string
	ErrorDescription  string
	CreatedAt         time.Time
}

// RazorpayWebhookStatus reports recent webhook activity.
type RazorpayWebhookStatus struct {
	LastReceivedAt time.Time        `json:"last_received_at"`
	Counts         map[string]int64 `json:"counts"`
}

type processedEvent struct {
	at time.Time
	id paymentagg.PaymentID
}

// RazorpayWebhookUseCase verifies and applies Razorpay payment webhooks.
type RazorpayWebhookUseCase struct {
	recordUC  *RecordPaymentUseCase
	reverseUC *ReversePaymentUseCase
	uow       ports.UnitOfWork
	secret    string
	clock     ports.Clock
	eventBus  events.EventBus

	processedMu  sync.Mutex
	processedIDs map[string]processedEvent

	statsMu        sync.RWMutex
	lastReceivedAt time.Time
	counts         map[string]int64
}

// NewRazorpayWebhookUseCase constructs a RazorpayWebhookUseCase.
func NewRazorpayWebhookUseCase(recordUC *RecordPaymentUseCase, uow ports.UnitOfWork, webhookSecret string, clock ports.Clock) *RazorpayWebhookUseCase {
	return &RazorpayWebhookUseCase{
		recordUC:     recordUC,
		uow:          uow,
		secret:       webhookSecret,
		clock:        clock,
		processedIDs: make(map[string]processedEvent),
		counts:       make(map[string]int64),
	}
}

// SetReversePaymentUseCase wires the reversal use case used for refunds.
func (uc *RazorpayWebhookUseCase) SetReversePaymentUseCase(reverseUC *ReversePaymentUseCase) {
	uc.reverseUC = reverseUC
}

// SetEventBus wires an optional event bus for emitting failed-payment events.
func (uc *RazorpayWebhookUseCase) SetEventBus(bus events.EventBus) {
	uc.eventBus = bus
}

// VerifySignature validates the HMAC-SHA256 signature over the raw body.
func (uc *RazorpayWebhookUseCase) VerifySignature(rawBody []byte, signature string) error {
	if uc.secret == "" {
		return ErrWebhookNotConfigured
	}
	if signature == "" {
		return ErrWebhookInvalidSignature
	}
	want, err := hex.DecodeString(signature)
	if err != nil {
		return ErrWebhookInvalidSignature
	}
	mac := hmac.New(sha256.New, []byte(uc.secret))
	mac.Write(rawBody)
	if !hmac.Equal(want, mac.Sum(nil)) {
		return ErrWebhookInvalidSignature
	}
	return nil
}

// Execute verifies the webhook signature and records the payment idempotently.
// Non-payment events are acknowledged without side effects.
func (uc *RazorpayWebhookUseCase) Execute(ctx context.Context, rawBody []byte, signature string) (paymentagg.PaymentID, error) {
	var ev RazorpayWebhookEvent
	return uc.ExecuteEvent(ctx, rawBody, signature, "", ev)
}

// ExecuteEvent verifies the webhook signature, enforces event-id idempotency
// (in-memory hot cache + restart-safe DB layer), and applies the side effects.
func (uc *RazorpayWebhookUseCase) ExecuteEvent(ctx context.Context, rawBody []byte, signature string, eventID string, ev RazorpayWebhookEvent) (paymentagg.PaymentID, error) {
	if err := uc.VerifySignature(rawBody, signature); err != nil {
		return "", err
	}
	if ev.Event == "" {
		if err := json.Unmarshal(rawBody, &ev); err != nil {
			return "", err
		}
	}

	uc.touchLastReceived()

	// Webhook-event bookkeeping is platform-global: public route carries no
	// request tenant, and event-id dedup must hold regardless of which invoice
	// the payload references.
	tenantID := fallbackTenant(ctx)

	// In-memory hot cache (survives within a process).
	if id, ok := uc.isProcessed(eventID); ok {
		return id, nil
	}

	// Restart-safe: an event ID already persisted must not be applied twice.
	if eventID != "" {
		existing, err := uc.findWebhookEvent(ctx, tenantID, eventID)
		if err != nil {
			return "", err
		}
		if existing != "" {
			uc.markProcessed(eventID, existing)
			return existing, nil
		}
	}

	var id paymentagg.PaymentID
	var err error

	switch ev.Event {
	case "payment.captured":
		id, err = uc.recordPaymentEntity(ctx, ev.Payload.Payment.Entity)
	case "order.paid":
		id, err = uc.recordOrderEntity(ctx, ev.Payload.Order.Entity)
	case "refund.processed":
		id, err = uc.processRefundEntity(ctx, ev.Payload.Refund.Entity)
	case "payment.failed":
		err = uc.processFailed(ctx, eventID, ev)
	default:
		// Non-payment events are acknowledged without side effects.
	}

	if err != nil {
		return "", err
	}

	// Restart-safe dedup layer: persist the event ID on the recorded payment.
	if id != "" && eventID != "" {
		if err := uc.setWebhookEventID(ctx, tenantID, id, eventID); err != nil {
			return "", err
		}
	}

	uc.markProcessed(eventID, id)
	uc.incrementCount(ev.Event)
	return id, nil
}

// Status returns the last received timestamp and processed counts by event type.
func (uc *RazorpayWebhookUseCase) Status() RazorpayWebhookStatus {
	uc.statsMu.RLock()
	defer uc.statsMu.RUnlock()

	counts := make(map[string]int64, len(uc.counts))
	for k, v := range uc.counts {
		counts[k] = v
	}
	return RazorpayWebhookStatus{
		LastReceivedAt: uc.lastReceivedAt,
		Counts:         counts,
	}
}

// invoiceTenantSource is the optional capability on the invoices repository
// used to attribute webhook records when no request tenant exists.
type invoiceTenantSource interface {
	TenantForInvoice(ctx context.Context, invoiceID string) (shared.TenantID, error)
}

// paymentReferenceTenantSource is the optional capability on the payments
// repository used to route refund webhooks back to the original payment's
// tenant.
type paymentReferenceTenantSource interface {
	FindReferenceTenant(ctx context.Context, reference string) (shared.TenantID, error)
}

// fallbackTenant returns the request-context tenant, or the bootstrap default
// for platform-global bookkeeping rows written from the public webhook route.
func fallbackTenant(ctx context.Context) shared.TenantID {
	if tid := shared.TenantIDFromContext(ctx); tid != "" {
		return tid
	}
	return shared.DefaultTenant
}

// resolveInvoiceTenant attributes a webhook payload to the invoice's owning
// tenant. The Razorpay route is public (signature-authenticated only), so the
// request context carries no tenant — the referenced record is authoritative.
// Returns "" when the invoice does not exist; callers keep their existing
// not-attributable handling instead of guessing a tenant.
func (uc *RazorpayWebhookUseCase) resolveInvoiceTenant(ctx context.Context, invoiceID string) shared.TenantID {
	if uc.uow == nil || invoiceID == "" {
		return ""
	}
	var out shared.TenantID
	_ = uc.uow.Execute(ctx, func(txCtx ports.TxContext) error {
		src, ok := txCtx.Repositories().Invoices().(invoiceTenantSource)
		if !ok {
			return errors.New("invoices repository lacks TenantForInvoice capability")
		}
		tid, err := src.TenantForInvoice(txCtx, invoiceID)
		if err != nil {
			return err
		}
		out = tid
		return nil
	})
	return out
}

// resolvePaymentReferenceTenant mirrors resolveInvoiceTenant for refund flows,
// discovering tenancy from the original gateway payment reference.
func (uc *RazorpayWebhookUseCase) resolvePaymentReferenceTenant(ctx context.Context, reference string) shared.TenantID {
	if uc.uow == nil || reference == "" {
		return ""
	}
	var out shared.TenantID
	_ = uc.uow.Execute(ctx, func(txCtx ports.TxContext) error {
		src, ok := txCtx.Repositories().Payments().(paymentReferenceTenantSource)
		if !ok {
			return errors.New("payments repository lacks FindReferenceTenant capability")
		}
		tid, err := src.FindReferenceTenant(txCtx, reference)
		if err != nil {
			return err
		}
		out = tid
		return nil
	})
	return out
}

func (uc *RazorpayWebhookUseCase) recordPaymentEntity(ctx context.Context, entity RazorpayPaymentEntity) (paymentagg.PaymentID, error) {
	if entity.ID == "" {
		return "", ErrWebhookInvoiceMissing
	}

	if entity.Notes.InvoiceID == "" {
		// Cannot attribute the payment to an invoice. Acknowledge the webhook
		// (200, no retry storm) and surface an alert — the payment must not be
		// silently dropped (Spec 11 §5.1).
		uc.acknowledgeUnattributable(ctx, "payment.captured", entity.ID, "")
		return "", nil
	}

	// Public route: tenancy comes from the referenced invoice, never ctx.
	tenantID := uc.resolveInvoiceTenant(ctx, entity.Notes.InvoiceID)
	if tenantID == "" {
		slog.Default().Warn("razorpay webhook references unknown invoice; acknowledged without recording",
			"payment_id", entity.ID, "invoice_id", entity.Notes.InvoiceID)
		return "", nil
	}

	// Race with the /verify flow: if the payment was already recorded via
	// checkout verification, return the existing payment — never double count.
	existing, err := uc.findRazorpayPayment(ctx, tenantID, entity.ID)
	if err != nil {
		return "", err
	}
	if existing != "" {
		return existing, nil
	}

	if entity.Notes.InvoiceID == "" {
		// Cannot attribute the payment to an invoice. Acknowledge the webhook
		// (200, no retry storm) and surface an alert — the payment must not be
		// silently dropped (Spec 11 §5.1).
		uc.acknowledgeUnattributable(ctx, "payment.captured", entity.ID, "")
		return "", nil
	}

	existingByRef, err := uc.findByReference(ctx, entity.ID, tenantID)
	if err != nil {
		return "", err
	}
	if existingByRef != "" {
		return existingByRef, nil
	}

	reference := entity.ID
	return uc.recordUC.Execute(ctx, RecordPaymentCommand{
		TenantID:    tenantID,
		InvoiceID:   entity.Notes.InvoiceID,
		PaymentDate: uc.clock.Now(),
		Amount:      float64(entity.Amount) / 100,
		Method:      paymentagg.PaymentMethodRazorpay,
		Reference:   &reference,
	})
}

func (uc *RazorpayWebhookUseCase) recordOrderEntity(ctx context.Context, entity RazorpayOrderEntity) (paymentagg.PaymentID, error) {
	if entity.ID == "" {
		return "", ErrWebhookInvoiceMissing
	}

	if entity.Notes.InvoiceID == "" {
		uc.acknowledgeUnattributable(ctx, "order.paid", entity.ID, "")
		return "", nil
	}

	// Public route: tenancy comes from the referenced invoice, never ctx.
	tenantID := uc.resolveInvoiceTenant(ctx, entity.Notes.InvoiceID)
	if tenantID == "" {
		slog.Default().Warn("razorpay webhook order references unknown invoice; acknowledged without recording",
			"order_id", entity.ID, "invoice_id", entity.Notes.InvoiceID)
		return "", nil
	}

	existing, err := uc.findByReference(ctx, entity.ID, tenantID)
	if err != nil {
		return "", err
	}
	if existing != "" {
		return existing, nil
	}

	reference := entity.ID
	return uc.recordUC.Execute(ctx, RecordPaymentCommand{
		TenantID:    tenantID,
		InvoiceID:   entity.Notes.InvoiceID,
		PaymentDate: uc.clock.Now(),
		Amount:      float64(entity.AmountPaid) / 100,
		Method:      paymentagg.PaymentMethodRazorpay,
		Reference:   &reference,
	})
}

// acknowledgeUnattributable logs a warning and emits a payment-failed alert
// when a webhook payload cannot be linked to an invoice. The webhook itself is
// acknowledgeUnattributable logs a warning and emits a payment-failed alert
// so ops can reconcile manually. The webhook itself is still answered with
// HTTP 200 so Razorpay stops retrying.
func (uc *RazorpayWebhookUseCase) acknowledgeUnattributable(ctx context.Context, eventType, paymentID, invoiceID string) {
	slog.Default().Warn("razorpay webhook payment has no notes.invoice_id; acknowledged without recording",
		"event", eventType, "payment_id", paymentID, "invoice_id", invoiceID)
	if uc.eventBus == nil {
		return
	}
	uc.eventBus.Publish(ctx, events.Event{
		Type: events.RazorpayPaymentFailed,
		Payload: RazorpayPaymentFailedEvent{
			RazorpayPaymentID: paymentID,
			InvoiceID:         invoiceID,
			ErrorCode:         "INVOICE_UNATTRIBUTABLE",
			ErrorDescription:  "webhook payment missing notes.invoice_id",
			CreatedAt:         uc.clock.Now(),
		},
	})
}

func (uc *RazorpayWebhookUseCase) processRefundEntity(ctx context.Context, entity RazorpayRefundEntity) (paymentagg.PaymentID, error) {
	if entity.PaymentID == "" {
		return "", ErrWebhookOriginalPaymentNotFound
	}
	if uc.reverseUC == nil {
		return "", errors.New("reverse payment use case not configured")
	}

	// Refunds route to the original payment's tenant — the public webhook
	// context carries none (Spec 24 §Business logic).
	tenantID := uc.resolvePaymentReferenceTenant(ctx, entity.PaymentID)
	if tenantID == "" {
		return "", ErrWebhookOriginalPaymentNotFound
	}

	originalID, err := uc.findByReference(ctx, entity.PaymentID, tenantID)
	if err != nil {
		return "", err
	}
	if originalID == "" {
		return "", ErrWebhookOriginalPaymentNotFound
	}

	reason := fmt.Sprintf("Razorpay refund %s", entity.ID)
	return uc.reverseUC.Execute(ctx, ReversePaymentCommand{
		TenantID:      tenantID,
		OriginalPayID: originalID,
		Reason:        reason,
	})
}

func (uc *RazorpayWebhookUseCase) processFailed(ctx context.Context, eventID string, ev RazorpayWebhookEvent) error {
	entity := ev.Payload.Payment.Entity
	if uc.eventBus == nil {
		return nil
	}

	uc.eventBus.Publish(ctx, events.Event{
		Type: events.RazorpayPaymentFailed,
		Payload: RazorpayPaymentFailedEvent{
			RazorpayEventID:   eventID,
			RazorpayPaymentID: entity.ID,
			OrderID:           entity.OrderID,
			InvoiceID:         entity.Notes.InvoiceID,
			Amount:            float64(entity.Amount) / 100,
			Currency:          entity.Currency,
			ErrorCode:         entity.ErrorCode,
			ErrorDescription:  entity.ErrorDescription,
			CreatedAt:         uc.clock.Now(),
		},
	})
	return nil
}

func (uc *RazorpayWebhookUseCase) findByReference(ctx context.Context, reference string, tenantID shared.TenantID) (paymentagg.PaymentID, error) {
	var found paymentagg.PaymentID
	err := uc.uow.Execute(ctx, func(txCtx ports.TxContext) error {
		payRepo, ok := txCtx.Repositories().Payments().(domain.PaymentRepository)
		if !ok {
			return errors.New("failed to retrieve payment repository")
		}
		id, err := payRepo.FindByReference(txCtx, reference, tenantID)
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

func (uc *RazorpayWebhookUseCase) findRazorpayPayment(ctx context.Context, tenantID shared.TenantID, paymentID string) (paymentagg.PaymentID, error) {
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

func (uc *RazorpayWebhookUseCase) findWebhookEvent(ctx context.Context, tenantID shared.TenantID, eventID string) (paymentagg.PaymentID, error) {
	var found paymentagg.PaymentID
	err := uc.uow.Execute(ctx, func(txCtx ports.TxContext) error {
		payRepo, ok := txCtx.Repositories().Payments().(domain.PaymentRepository)
		if !ok {
			return errors.New("failed to retrieve payment repository")
		}
		id, err := payRepo.ExistsWebhookEvent(txCtx, tenantID, eventID)
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

func (uc *RazorpayWebhookUseCase) setWebhookEventID(ctx context.Context, tenantID shared.TenantID, id paymentagg.PaymentID, eventID string) error {
	return uc.uow.Execute(ctx, func(txCtx ports.TxContext) error {
		payRepo, ok := txCtx.Repositories().Payments().(domain.PaymentRepository)
		if !ok {
			return errors.New("failed to retrieve payment repository")
		}
		return payRepo.SetWebhookEventID(txCtx, id, tenantID, eventID)
	})
}

func (uc *RazorpayWebhookUseCase) isProcessed(eventID string) (paymentagg.PaymentID, bool) {
	if eventID == "" {
		return "", false
	}
	uc.processedMu.Lock()
	defer uc.processedMu.Unlock()

	uc.cleanProcessedLocked()
	p, ok := uc.processedIDs[eventID]
	return p.id, ok
}

func (uc *RazorpayWebhookUseCase) markProcessed(eventID string, id paymentagg.PaymentID) {
	if eventID == "" {
		return
	}
	uc.processedMu.Lock()
	defer uc.processedMu.Unlock()

	if uc.processedIDs == nil {
		uc.processedIDs = make(map[string]processedEvent)
	}
	uc.processedIDs[eventID] = processedEvent{at: uc.clock.Now(), id: id}
}

func (uc *RazorpayWebhookUseCase) cleanProcessedLocked() {
	now := uc.clock.Now()
	for id, p := range uc.processedIDs {
		if now.Sub(p.at) >= webhookEventTTL {
			delete(uc.processedIDs, id)
		}
	}
}

func (uc *RazorpayWebhookUseCase) touchLastReceived() {
	uc.statsMu.Lock()
	defer uc.statsMu.Unlock()
	uc.lastReceivedAt = uc.clock.Now()
}

func (uc *RazorpayWebhookUseCase) incrementCount(eventType string) {
	uc.statsMu.Lock()
	defer uc.statsMu.Unlock()
	if uc.counts == nil {
		uc.counts = make(map[string]int64)
	}
	uc.counts[eventType]++
}
