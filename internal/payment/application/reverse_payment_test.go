package application

import (
	"testing"

	paymentagg "transport-app/internal/payment/domain/aggregate"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReversePayment_RetryIsIdempotent(t *testing.T) {
	ctx, uow, _, reverseUC, _, clock, _ := setupWebhookUnitTest(t)

	invID := seedInvoice(t, uow.repos.invoices, clock, 1000)

	payID := paymentagg.PaymentID("pay-orig-1")
	original := paymentagg.NewPaymentAggregate(
		payID,
		"1",
		string(invID),
		clock.Now(),
		400,
		paymentagg.PaymentMethodCash,
		nil,
		nil,
		clock.Now(),
	)
	require.NoError(t, uow.repos.payments.Save(ctx, original))

	inv, err := uow.repos.invoices.Find(ctx, invID, "1")
	require.NoError(t, err)
	require.NoError(t, inv.ApplyPayment(400, clock.Now()))
	require.NoError(t, uow.repos.invoices.Save(ctx, inv))

	cmd := ReversePaymentCommand{TenantID: "1", OriginalPayID: payID, Reason: "customer refund"}

	id1, err := reverseUC.Execute(ctx, cmd)
	require.NoError(t, err)
	require.NotEmpty(t, id1)

	invAfterFirst, err := uow.repos.invoices.Find(ctx, invID, "1")
	require.NoError(t, err)
	assert.Equal(t, 0.0, invAfterFirst.PaidAmount, "first reversal decrements paid amount to zero")

	id2, err := reverseUC.Execute(ctx, cmd)
	require.NoError(t, err)
	assert.Equal(t, id1, id2, "retried reversal must return the existing reversal id")

	invAfterRetry, err := uow.repos.invoices.Find(ctx, invID, "1")
	require.NoError(t, err)
	assert.Equal(t, 0.0, invAfterRetry.PaidAmount, "retry must not double-decrement paid amount")
}

func TestReversePayment_ValidationErrors(t *testing.T) {
	ctx, _, _, reverseUC, _, _, _ := setupWebhookUnitTest(t)

	_, err := reverseUC.Execute(ctx, ReversePaymentCommand{TenantID: "1", Reason: "r"})
	assert.ErrorIs(t, err, ErrMissingOriginalPayment)

	_, err = reverseUC.Execute(ctx, ReversePaymentCommand{TenantID: "1", OriginalPayID: "p1"})
	assert.ErrorIs(t, err, ErrMissingReversalReason)
}

// Two different partial refunds on the same payment must both be recorded,
// and cumulative refunds must not exceed the original amount.
func TestReversePayment_MultiplePartialsAreRecordedAndCapped(t *testing.T) {
	ctx, uow, _, reverseUC, _, clock, _ := setupWebhookUnitTest(t)
	invID := seedInvoice(t, uow.repos.invoices, clock, 1000)
	payID := paymentagg.PaymentID("pay-partials")
	original := paymentagg.NewPaymentAggregate(payID, "1", string(invID), clock.Now(), 400,
		paymentagg.PaymentMethodCash, nil, nil, clock.Now())
	require.NoError(t, uow.repos.payments.Save(ctx, original))
	inv, err := uow.repos.invoices.Find(ctx, invID, "1")
	require.NoError(t, err)
	require.NoError(t, inv.ApplyPayment(400, clock.Now()))
	require.NoError(t, uow.repos.invoices.Save(ctx, inv))

	id1, err := reverseUC.Execute(ctx, ReversePaymentCommand{TenantID: "1", OriginalPayID: payID, Reason: "partial 1", Amount: 100})
	require.NoError(t, err)
	id2, err := reverseUC.Execute(ctx, ReversePaymentCommand{TenantID: "1", OriginalPayID: payID, Reason: "partial 2", Amount: 150})
	require.NoError(t, err)
	require.NotEqual(t, id1, id2, "a second partial refund must not be dropped as a retry")

	after, err := uow.repos.invoices.Find(ctx, invID, "1")
	require.NoError(t, err)
	require.InDelta(t, 150.0, after.PaidAmount, 0.01)

	// Over-refund is capped at the remaining 150.
	_, err = reverseUC.Execute(ctx, ReversePaymentCommand{TenantID: "1", OriginalPayID: payID, Reason: "over", Amount: 500})
	require.NoError(t, err)
	after, err = uow.repos.invoices.Find(ctx, invID, "1")
	require.NoError(t, err)
	require.InDelta(t, 0.0, after.PaidAmount, 0.01)
}
