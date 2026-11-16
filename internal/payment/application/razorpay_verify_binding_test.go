package application

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	invoiceagg "transport-app/internal/invoice/domain/aggregate"
	"transport-app/internal/payment/razorpay"
	"transport-app/internal/shared"
)

// fakeGatewayApp is the test double for razorpay.PaymentGateway. It returns
// canned gateway state so the binding checks can be exercised without network
// access.
type fakeGatewayApp struct {
	payment  *razorpay.PaymentDetails
	order    *razorpay.OrderDetails
	payErr   error
	orderErr error
}

func (f *fakeGatewayApp) FetchPayment(string) (*razorpay.PaymentDetails, error) {
	if f.payErr != nil {
		return nil, f.payErr
	}
	return f.payment, nil
}

func (f *fakeGatewayApp) FetchOrder(string) (*razorpay.OrderDetails, error) {
	if f.orderErr != nil {
		return nil, f.orderErr
	}
	return f.order, nil
}

// setupVerifyBinding builds a use case over an invoice of `total` INR. Tests
// attach a gateway with WithPaymentGateway once the invoice ID is known.
func setupVerifyBinding(t *testing.T, total float64) (*fakeUnitOfWork, *VerifyRazorpayPaymentUseCase, string, *fakeClock) {
	t.Helper()
	clock := &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	uow := newFakeUnitOfWork()
	invID := seedInvoice(t, uow.repos.invoices, clock, total)
	recordUC := NewRecordPaymentUseCase(uow, &fakeIDGen{}, clock)
	uc := NewVerifyRazorpayPaymentUseCase(uow, recordUC, &fakeVerifier{ok: true}, "secret", clock)
	return uow, uc, string(invID), clock
}

// boundGateway builds a gateway response for a captured 1000 INR payment whose
// order is bound to invoiceID.
func boundGateway(invoiceID string) *fakeGatewayApp {
	const paise = int64(100000) // 1000 INR
	return &fakeGatewayApp{
		payment: &razorpay.PaymentDetails{
			ID:       "pay_bound",
			OrderID:  "order_bound",
			Status:   "captured",
			Currency: "INR",
			Amount:   paise,
		},
		order: &razorpay.OrderDetails{
			ID:      "order_bound",
			Status:  "paid",
			Receipt: "rcpt_" + invoiceID,
			Amount:  paise,
			Notes:   map[string]string{"invoice_id": invoiceID},
		},
	}
}

func verifyCmd(invoiceID string) VerifyRazorpayPaymentCommand {
	return VerifyRazorpayPaymentCommand{
		TenantID:  "1",
		InvoiceID: invoiceID,
		OrderID:   "order_bound",
		PaymentID: "pay_bound",
		Signature: "sig_bound",
	}
}

func verifyCtx() context.Context {
	return shared.ContextWithTenantID(context.Background(), "1")
}

func paymentCount(t *testing.T, uow *fakeUnitOfWork, invoiceID string) int {
	t.Helper()
	payments, err := uow.repos.payments.GetPaymentsByInvoice(verifyCtx(), invoiceID, "1")
	require.NoError(t, err)
	return len(payments)
}

func invoicePaidAmount(t *testing.T, uow *fakeUnitOfWork, invoiceID string) float64 {
	t.Helper()
	inv, err := uow.repos.invoices.Find(verifyCtx(), invoiceagg.InvoiceID(invoiceID), "1")
	require.NoError(t, err)
	return inv.PaidAmount
}

// TestVerifyRazorpayPayment_BindsOrderToInvoice is the happy path: signature
// verifies, payment belongs to the order, order belongs to the invoice, amount
// matches and is captured — the invoice is credited exactly once.
func TestVerifyRazorpayPayment_BindsOrderToInvoice(t *testing.T) {
	uow, uc, invID, _ := setupVerifyBinding(t, 1000)
	uc.WithPaymentGateway(boundGateway(invID))

	id, err := uc.Execute(verifyCtx(), verifyCmd(invID))
	require.NoError(t, err)
	require.NotEmpty(t, id)

	assert.Equal(t, 1000.0, invoicePaidAmount(t, uow, invID))
	assert.Equal(t, 1, paymentCount(t, uow, invID))
}

// TestVerifyRazorpayPayment_RejectsOrderBoundToAnotherInvoice is the red test
// for the original vulnerability: a valid signature tuple from a payment whose
// order was created for invoice A must never credit invoice B.
func TestVerifyRazorpayPayment_RejectsOrderBoundToAnotherInvoice(t *testing.T) {
	uow, uc, invID, _ := setupVerifyBinding(t, 1000)
	uc.WithPaymentGateway(boundGateway("some-other-invoice"))

	_, err := uc.Execute(verifyCtx(), verifyCmd(invID))
	require.ErrorIs(t, err, ErrRazorpayInvoiceMismatch)
	assert.Equal(t, 0, paymentCount(t, uow, invID), "no payment may be recorded")
	assert.Equal(t, 0.0, invoicePaidAmount(t, uow, invID), "invoice must stay unpaid")
}

// TestVerifyRazorpayPayment_RejectsPaymentForDifferentOrder proves the payment
// must belong to the exact order supplied by the caller.
func TestVerifyRazorpayPayment_RejectsPaymentForDifferentOrder(t *testing.T) {
	uow, uc, invID, _ := setupVerifyBinding(t, 1000)
	gw := boundGateway(invID)
	gw.payment.OrderID = "order_someone_else"
	uc.WithPaymentGateway(gw)

	_, err := uc.Execute(verifyCtx(), verifyCmd(invID))
	require.ErrorIs(t, err, ErrRazorpayOrderMismatch)
	assert.Equal(t, 0, paymentCount(t, uow, invID))
}

// TestVerifyRazorpayPayment_RejectsUncapturedPayment proves an authorized (not
// captured) payment is not recorded as collected money.
func TestVerifyRazorpayPayment_RejectsUncapturedPayment(t *testing.T) {
	uow, uc, invID, _ := setupVerifyBinding(t, 1000)
	gw := boundGateway(invID)
	gw.payment.Status = "authorized"
	uc.WithPaymentGateway(gw)

	_, err := uc.Execute(verifyCtx(), verifyCmd(invID))
	require.ErrorIs(t, err, ErrRazorpayPaymentNotCaptured)
	assert.Equal(t, 0, paymentCount(t, uow, invID))
}

// TestVerifyRazorpayPayment_RejectsAmountMismatch proves the captured amount
// must equal the order amount.
func TestVerifyRazorpayPayment_RejectsAmountMismatch(t *testing.T) {
	uow, uc, invID, _ := setupVerifyBinding(t, 1000)
	gw := boundGateway(invID)
	gw.payment.Amount = 100 // 1 INR against a 1000 INR order
	uc.WithPaymentGateway(gw)

	_, err := uc.Execute(verifyCtx(), verifyCmd(invID))
	require.ErrorIs(t, err, ErrRazorpayAmountMismatch)
	assert.Equal(t, 0, paymentCount(t, uow, invID))
}

// TestVerifyRazorpayPayment_RecordsCapturedAmountNotBalance proves the payment
// row carries the captured amount, not the invoice's current balance.
func TestVerifyRazorpayPayment_RecordsCapturedAmountNotBalance(t *testing.T) {
	uow, uc, invID, _ := setupVerifyBinding(t, 1000)
	gw := boundGateway(invID)
	gw.payment.Amount = 40000 // 400 INR
	gw.order.Amount = 40000
	uc.WithPaymentGateway(gw)

	_, err := uc.Execute(verifyCtx(), verifyCmd(invID))
	require.NoError(t, err)

	assert.Equal(t, 400.0, invoicePaidAmount(t, uow, invID),
		"captured 400 INR must be recorded, not the 1000 INR balance")
	inv, err := uow.repos.invoices.Find(verifyCtx(), invoiceagg.InvoiceID(invID), "1")
	require.NoError(t, err)
	assert.Equal(t, invoiceagg.PaymentStatusPartiallyPaid, inv.PaymentStatus)
}

// TestVerifyRazorpayPayment_FailsClosedWithoutGateway proves a missing gateway
// reader can never fall back to signature-only verification.
func TestVerifyRazorpayPayment_FailsClosedWithoutGateway(t *testing.T) {
	uow, uc, invID, _ := setupVerifyBinding(t, 1000)
	_ = uow

	_, err := uc.Execute(verifyCtx(), verifyCmd(invID))
	require.ErrorIs(t, err, ErrRazorpayGatewayNotConfigured)
}
