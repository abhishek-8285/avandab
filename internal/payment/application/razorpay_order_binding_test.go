package application

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	paymentagg "transport-app/internal/payment/domain/aggregate"
	"transport-app/internal/payment/orderstore"
	"transport-app/internal/shared"
)

type fakeOrderBindingStore struct {
	bindings map[string]orderstore.Binding
}

func (f *fakeOrderBindingStore) SaveOrder(context.Context, string, string, string, int64, string) error {
	return nil
}

func (f *fakeOrderBindingStore) GetOrder(_ context.Context, orderID string) (orderstore.Binding, error) {
	b, ok := f.bindings[orderID]
	if !ok {
		return orderstore.Binding{}, sql.ErrNoRows
	}
	return b, nil
}

// An order that was created for invoice A must never settle invoice B, even
// when its order|payment signature is valid.
func TestVerifyRazorpayPayment_WithOrderBindingRejectsForeignInvoice(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	uow := newFakeUnitOfWork()
	invoiceID := seedInvoice(t, uow.repos.invoices, clock, 1000)
	recordUC := NewRecordPaymentUseCase(uow, &fakeIDGen{}, clock)

	store := &fakeOrderBindingStore{bindings: map[string]orderstore.Binding{
		"order_foreign": {TenantID: "1", InvoiceID: "inv-other", AmountPaise: 100},
	}}
	uc := NewVerifyRazorpayPaymentUseCase(uow, recordUC, &fakeVerifier{ok: true}, "secret", clock).
		WithOrderBinding(store)

	ctx := context.WithValue(context.Background(), shared.TenantIDKey, shared.TenantID("1"))
	_, err := uc.Execute(ctx, VerifyRazorpayPaymentCommand{
		TenantID:  "1",
		InvoiceID: string(invoiceID),
		OrderID:   "order_foreign",
		PaymentID: "pay_foreign",
		Signature: "sig",
	})
	require.ErrorIs(t, err, ErrInvoiceNotFound)

	payments, err := uow.repos.payments.GetPaymentsByInvoice(ctx, string(invoiceID), "1")
	require.NoError(t, err)
	require.Empty(t, payments, "no payment may be recorded for a mismatched order")

	_ = paymentagg.PaymentMethodRazorpay
}

// The recorded payment must be the captured amount, not the invoice balance.
func TestVerifyRazorpayPayment_RecordsBoundAmount(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	uow := newFakeUnitOfWork()
	invoiceID := seedInvoice(t, uow.repos.invoices, clock, 1000)
	recordUC := NewRecordPaymentUseCase(uow, &fakeIDGen{}, clock)
	store := &fakeOrderBindingStore{bindings: map[string]orderstore.Binding{
		"order-partial": {TenantID: "1", InvoiceID: string(invoiceID), AmountPaise: 10000},
	}}
	uc := NewVerifyRazorpayPaymentUseCase(uow, recordUC, &fakeVerifier{ok: true}, "secret", clock).
		WithOrderBinding(store)

	ctx := context.WithValue(context.Background(), shared.TenantIDKey, shared.TenantID("1"))
	_, err := uc.Execute(ctx, VerifyRazorpayPaymentCommand{
		TenantID: "1", InvoiceID: string(invoiceID), OrderID: "order-partial",
		PaymentID: "pay_partial", Signature: "sig",
	})
	require.NoError(t, err)

	inv, err := uow.repos.invoices.Find(ctx, invoiceID, "1")
	require.NoError(t, err)
	require.InDelta(t, 100.0, inv.PaidAmount, 0.01, "only the captured amount may be credited")
}
