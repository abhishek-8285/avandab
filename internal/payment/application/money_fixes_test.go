package application

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/events"
	paymentagg "transport-app/internal/payment/domain/aggregate"
)

// orderPaidWebhookBody builds an order.paid payload for the double-credit test.
func orderPaidWebhookBody(orderID, invoiceID string, amountPaise int64) []byte {
	payload := map[string]interface{}{
		"event": "order.paid",
		"payload": map[string]interface{}{
			"order": map[string]interface{}{
				"entity": map[string]interface{}{
					"id": orderID, "amount_paid": amountPaise, "currency": "INR",
					"status": "paid",
					"notes":  map[string]interface{}{"invoice_id": invoiceID},
				},
			},
		},
	}
	b, _ := json.Marshal(payload)
	return b
}

// Fix #5: one checkout fires payment.captured AND order.paid. Both must
// resolve to a single recorded payment, never two credits to the invoice.
func TestRazorpayWebhook_CapturedThenOrderPaid_NoDoubleCredit(t *testing.T) {
	ctx, uow, _, _, webhookUC, clock, _ := setupWebhookUnitTest(t)
	invID := seedInvoice(t, uow.repos.invoices, clock, 1000)

	captured := capturedWebhookBody("pay_checkout_5", string(invID), 100000)
	_, err := webhookUC.ExecuteEvent(ctx, captured, signWebhook(t, captured, testWebhookSecret), "evt_captured_5", RazorpayWebhookEvent{})
	require.NoError(t, err)

	paid := orderPaidWebhookBody("order_checkout_5", string(invID), 100000)
	_, err = webhookUC.ExecuteEvent(ctx, paid, signWebhook(t, paid, testWebhookSecret), "evt_orderpaid_5", RazorpayWebhookEvent{})
	require.NoError(t, err)

	payments, err := uow.repos.payments.GetPaymentsByInvoice(ctx, string(invID), "1")
	require.NoError(t, err)
	require.Len(t, payments, 1, "captured + order.paid for one checkout must record exactly one payment")
}

// Fix #6: a partial refund (500 of 1000) must reverse only 500, not the whole original.
func TestRazorpayWebhook_PartialRefund_ReversesOnlyRefundedAmount(t *testing.T) {
	ctx, uow, recordUC, _, webhookUC, clock, _ := setupWebhookUnitTest(t)
	invID := seedInvoice(t, uow.repos.invoices, clock, 1000)

	origRef := "pay_partial_6"
	_, err := recordUC.Execute(ctx, RecordPaymentCommand{
		TenantID: testTenantID, InvoiceID: string(invID), PaymentDate: clock.Now(),
		Amount: 1000, Method: paymentagg.PaymentMethodRazorpay, Reference: &origRef,
	})
	require.NoError(t, err)

	body := refundWebhookBody("rfnd_partial_6", origRef, 50000) // 500 INR in paise
	reversalID, err := webhookUC.ExecuteEvent(ctx, body, signWebhook(t, body, testWebhookSecret), "evt_refund_6", RazorpayWebhookEvent{})
	require.NoError(t, err)

	reversal, err := uow.repos.payments.Find(ctx, reversalID, testTenantID)
	require.NoError(t, err)
	require.NotNil(t, reversal)
	assert.Equal(t, -500.0, reversal.Amount, "partial refund must reverse exactly the refunded amount")
}

// Fix #6: a refund larger than the original is capped at the original amount.
func TestRazorpayWebhook_OverRefund_CappedAtOriginal(t *testing.T) {
	ctx, uow, recordUC, _, webhookUC, clock, _ := setupWebhookUnitTest(t)
	invID := seedInvoice(t, uow.repos.invoices, clock, 1000)

	origRef := "pay_overrefund_6"
	_, err := recordUC.Execute(ctx, RecordPaymentCommand{
		TenantID: testTenantID, InvoiceID: string(invID), PaymentDate: clock.Now(),
		Amount: 1000, Method: paymentagg.PaymentMethodRazorpay, Reference: &origRef,
	})
	require.NoError(t, err)

	body := refundWebhookBody("rfnd_over_6", origRef, 200000) // 2000 INR > original 1000
	reversalID, err := webhookUC.ExecuteEvent(ctx, body, signWebhook(t, body, testWebhookSecret), "evt_overrefund_6", RazorpayWebhookEvent{})
	require.NoError(t, err)

	reversal, err := uow.repos.payments.Find(ctx, reversalID, testTenantID)
	require.NoError(t, err)
	require.NotNil(t, reversal)
	assert.Equal(t, -1000.0, reversal.Amount, "refund must be capped at the original payment amount")
}

// Fix #7: event id must be derived from the signed body. Replaying the same
// signed webhook with the X-Razorpay-Event-Id header stripped must not
// duplicate side effects.
func TestRazorpayWebhook_ReplayWithoutHeader_NoDuplicateSideEffects(t *testing.T) {
	ctx, uow, _, _, webhookUC, clock, _ := setupWebhookUnitTest(t)
	invID := seedInvoice(t, uow.repos.invoices, clock, 1000)

	bus := events.NewInMemoryBus()
	failedCount := 0
	bus.Subscribe(events.RazorpayPaymentFailed, func(_ context.Context, _ events.Event) error {
		failedCount++
		return nil
	})
	webhookUC.SetEventBus(bus)

	body := failedWebhookBody("pay_replay_nohdr_7", string(invID), 10000)
	sig := signWebhook(t, body, testWebhookSecret)

	// Execute passes an empty event id (equivalent to a stripped header).
	_, err := webhookUC.Execute(ctx, body, sig)
	require.NoError(t, err)
	_, err = webhookUC.Execute(ctx, body, sig)
	require.NoError(t, err)

	assert.Equal(t, 1, failedCount, "replay without the header must not duplicate webhook side effects")
}
