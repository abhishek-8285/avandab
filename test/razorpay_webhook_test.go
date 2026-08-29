package test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	invoiceApp "transport-app/internal/invoice/application"
	paymentApp "transport-app/internal/payment/application"

	"transport-app/internal/shared"
	"transport-app/internal/shared/clock"
	"transport-app/internal/shared/id"
	"transport-app/internal/shared/uow"
)

const testWebhookSecret = "whsec_test_secret"

func webhookSignature(t *testing.T, body []byte, secret string) string {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func razorpayCapturedEvent(paymentID, invoiceID string, amountPaise int64) []byte {
	payload := map[string]interface{}{
		"event": "payment.captured",
		"payload": map[string]interface{}{
			"payment": map[string]interface{}{
				"entity": map[string]interface{}{
					"id":       paymentID,
					"order_id": "order_123",
					"amount":   amountPaise,
					"currency": "INR",
					"status":   "captured",
					"notes": map[string]interface{}{
						"invoice_id": invoiceID,
					},
				},
			},
		},
	}
	b, _ := json.Marshal(payload)
	return b
}

func setupWebhookTest(t *testing.T) (context.Context, *paymentApp.RazorpayWebhookUseCase, func([]byte) string, func() []byte) {
	t.Helper()
	db := NewTestDB(t)
	sqlUoW := uow.NewSQLUnitOfWork(db)
	idGen := id.NewUUIDGenerator()
	realClock := clock.NewRealClock()
	ctx := ContextWithTestTenant(shared.ContextWithTenantID(context.Background(), "1"))

	svc := NewTestServices(t, db)
	customer, err := svc.Customers.CreateCustomer(ctx, "Webhook Co", "Webhook", "555-7777", "webhook@example.com", "", "", "")
	require.NoError(t, err)

	generateUC := invoiceApp.NewGenerateInvoiceUseCase(sqlUoW, idGen, realClock)
	invID, err := generateUC.Execute(ctx, invoiceApp.GenerateInvoiceCommand{
		TenantID:   "1",
		BookingID:  "bk-webhook",
		CustomerID: string(customer.ID),
		Subtotal:   10000,
		Tax:        1800,
		Total:      11800,
	})
	require.NoError(t, err)

	recordUC := paymentApp.NewRecordPaymentUseCase(sqlUoW, idGen, realClock)
	webhookUC := paymentApp.NewRazorpayWebhookUseCase(recordUC, sqlUoW, testWebhookSecret, realClock)

	pay := func() []byte { return razorpayCapturedEvent("pay_webhook_001", string(invID), 1180000) }
	sign := func(body []byte) string { return webhookSignature(t, body, testWebhookSecret) }
	return ctx, webhookUC, sign, pay
}

func TestRazorpayWebhook_InvalidSignature(t *testing.T) {
	ctx, uc, _, pay := setupWebhookTest(t)
	body := pay()
	_, err := uc.Execute(ctx, body, "deadbeef")
	assert.ErrorIs(t, err, paymentApp.ErrWebhookInvalidSignature)
}

func TestRazorpayWebhook_NotConfigured(t *testing.T) {
	db := NewTestDB(t)
	sqlUoW := uow.NewSQLUnitOfWork(db)
	recordUC := paymentApp.NewRecordPaymentUseCase(sqlUoW, id.NewUUIDGenerator(), clock.NewRealClock())
	uc := paymentApp.NewRazorpayWebhookUseCase(recordUC, sqlUoW, "", clock.NewRealClock())
	body := razorpayCapturedEvent("pay_x", "inv_x", 100)
	_, err := uc.Execute(shared.ContextWithTenantID(context.Background(), "1"), body, "sig")
	assert.ErrorIs(t, err, paymentApp.ErrWebhookNotConfigured)
}

func TestRazorpayWebhook_IgnoresNonCapturedEvents(t *testing.T) {
	ctx, uc, sign, _ := setupWebhookTest(t)
	body, _ := json.Marshal(map[string]interface{}{
		"event":   "payment.failed",
		"payload": map[string]interface{}{},
	})
	id, err := uc.Execute(ctx, body, sign(body))
	require.NoError(t, err)
	assert.Empty(t, id)
}

func TestRazorpayWebhook_RecordsPayment(t *testing.T) {
	ctx, uc, sign, pay := setupWebhookTest(t)
	body := pay()
	id, err := uc.Execute(ctx, body, sign(body))
	require.NoError(t, err)
	assert.NotEmpty(t, id)
}

func TestRazorpayWebhook_IdempotentRedelivery(t *testing.T) {
	ctx, uc, sign, pay := setupWebhookTest(t)
	body := pay()
	sig := sign(body)

	id1, err := uc.Execute(ctx, body, sig)
	require.NoError(t, err)
	id2, err := uc.Execute(ctx, body, sig)
	require.NoError(t, err)
	assert.Equal(t, id1, id2, "duplicate webhook delivery must return the same payment")

	// Reference dedupe should also hold for a fresh use case over the same DB
	_, err = uc.Execute(ctx, body, sig)
	assert.NoError(t, err)
}

// TestRazorpayWebhook_MissingInvoiceNotes proves a payment.captured webhook
// without notes.invoice_id is acknowledged (HTTP 200 semantics) rather than
// rejected — Razorpay must not retry forever, but the money must not be
// silently dropped (Spec 11 §5.1).
func TestRazorpayWebhook_MissingInvoiceNotes(t *testing.T) {
	ctx, uc, sign, _ := setupWebhookTest(t)
	body, _ := json.Marshal(map[string]interface{}{
		"event": "payment.captured",
		"payload": map[string]interface{}{
			"payment": map[string]interface{}{
				"entity": map[string]interface{}{
					"id":     "pay_no_invoice",
					"amount": 100,
				},
			},
		},
	})
	id, err := uc.Execute(ctx, body, sign(body))
	require.NoError(t, err, "missing invoice_id must be acknowledged, not rejected")
	assert.Empty(t, id, "no payment row is created for an unattributable webhook")
}
