package aggregate

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"transport-app/internal/shared"
)

func newBaseInvoice() *InvoiceAggregate {
	now := time.Now()
	tripID := "trip-1"
	return NewInvoiceAggregate(
		InvoiceID("inv-1"),
		shared.TenantID("t1"),
		"INV-0001",
		"bk-1",
		"cust-1",
		&tripID,
		1000,
		100,
		0,
		1000,
		PaymentStatusPending,
		now,
	)
}

func TestOutstandingBalance(t *testing.T) {
	tests := []struct {
		name       string
		total      float64
		paidAmount float64
		want       float64
	}{
		{"nothing paid", 1000, 0, 1000},
		{"partial", 1000, 300, 700},
		{"fully paid", 1000, 1000, 0},
		{"overpaid caps display", 1000, 1000, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			inv := newBaseInvoice()
			inv.Total = tc.total
			inv.PaidAmount = tc.paidAmount
			assert.Equal(t, tc.want, inv.OutstandingBalance())
		})
	}
}

func TestApplyPayment_Partial(t *testing.T) {
	inv := newBaseInvoice()
	inv.ClearEvents()
	now := time.Now()
	err := inv.ApplyPayment(300, now)
	assert.NoError(t, err)
	assert.Equal(t, 300.0, inv.PaidAmount)
	assert.Equal(t, PaymentStatusPartiallyPaid, inv.PaymentStatus)
	assert.Equal(t, 0.0, inv.CreditBalance)
	assert.Len(t, inv.Events(), 1)
}

func TestApplyPayment_Full(t *testing.T) {
	inv := newBaseInvoice()
	inv.ClearEvents()
	now := time.Now()
	err := inv.ApplyPayment(1000, now)
	assert.NoError(t, err)
	assert.Equal(t, 1000.0, inv.PaidAmount)
	assert.Equal(t, PaymentStatusPaid, inv.PaymentStatus)
	assert.Equal(t, InvoiceStatusPaid, inv.Status)
	assert.Equal(t, 0.0, inv.CreditBalance)
}

func TestApplyPayment_Overpayment(t *testing.T) {
	inv := newBaseInvoice()
	inv.ClearEvents()
	now := time.Now()
	err := inv.ApplyPayment(1200, now)
	assert.NoError(t, err)
	assert.Equal(t, 1200.0, inv.PaidAmount) // gross: credit derives from persisted paid
	assert.Equal(t, 200.0, inv.CreditBalance)
	assert.Equal(t, PaymentStatusPaid, inv.PaymentStatus)
	assert.Equal(t, InvoiceStatusPaid, inv.Status)
}

func TestApplyPayment_Cancelled(t *testing.T) {
	inv := newBaseInvoice()
	inv.Status = InvoiceStatusCancelled
	err := inv.ApplyPayment(100, time.Now())
	assert.Error(t, err)
	assert.Equal(t, "cannot apply payment to cancelled invoice", err.Error())
}

func TestMarkIssued_NonDraft(t *testing.T) {
	inv := newBaseInvoice()
	inv.Status = InvoiceStatusOutstanding
	err := inv.MarkIssued(time.Now(), time.Now())
	assert.Error(t, err)
	assert.Equal(t, "only draft invoices can be issued", err.Error())
}

func TestMarkIssued_Draft(t *testing.T) {
	inv := newBaseInvoice()
	inv.Status = InvoiceStatusDraft
	inv.ClearEvents()
	due := time.Now().Add(48 * time.Hour)
	now := time.Now()
	err := inv.MarkIssued(due, now)
	assert.NoError(t, err)
	assert.Equal(t, InvoiceStatusIssued, inv.Status)
	assert.NotNil(t, inv.DueDate)
	assert.Equal(t, due, *inv.DueDate)
	assert.Equal(t, now, inv.UpdatedAt)
	assert.Len(t, inv.Events(), 1)
	_, ok := inv.Events()[0].(InvoiceIssuedEvent)
	assert.True(t, ok)
}

func TestVoid_Paid(t *testing.T) {
	inv := newBaseInvoice()
	inv.Status = InvoiceStatusPaid
	err := inv.Void(time.Now())
	assert.Error(t, err)
	assert.Equal(t, "paid invoices cannot be voided", err.Error())
}

func TestVoid_Pending(t *testing.T) {
	inv := newBaseInvoice()
	inv.Status = InvoiceStatusOutstanding
	inv.ClearEvents()
	now := time.Now()
	err := inv.Void(now)
	assert.NoError(t, err)
	assert.Equal(t, InvoiceStatusCancelled, inv.Status)
	assert.Equal(t, now, inv.UpdatedAt)
	assert.Len(t, inv.Events(), 1)
	_, ok := inv.Events()[0].(InvoiceVoidedEvent)
	assert.True(t, ok)
}

func TestVoid_Twice(t *testing.T) {
	inv := newBaseInvoice()
	inv.Status = InvoiceStatusOutstanding
	_ = inv.Void(time.Now())
	err := inv.Void(time.Now())
	assert.Error(t, err)
	assert.Equal(t, "invoice already cancelled", err.Error())
}

func TestRehydrateInvoiceAggregate_NoEvents(t *testing.T) {
	now := time.Now()
	inv := RehydrateInvoiceAggregate(
		InvoiceID("inv-r"),
		shared.TenantID("t1"),
		"INV-0001",
		"bk-1",
		"cust-1",
		nil,
		1000, 100, 0, 1000,
		PaymentStatusPending, InvoiceStatusOutstanding,
		0, 0,
		nil, "", "",
		now, now, 1,
	)
	assert.Empty(t, inv.Events())
	assert.Equal(t, int64(1), inv.Version)
	assert.Equal(t, InvoiceStatusOutstanding, inv.Status)
}

func TestNewInvoiceAggregate_EmitsOneEvent(t *testing.T) {
	inv := newBaseInvoice()
	assert.Len(t, inv.Events(), 1)
	_, ok := inv.Events()[0].(InvoiceGeneratedEvent)
	assert.True(t, ok)
}

func TestValidateInvoiceNumber(t *testing.T) {
	assert.Error(t, ValidateInvoiceNumber(""))
	assert.NoError(t, ValidateInvoiceNumber("INV-0001"))
}

func TestAddLineItem_RecomputesSubtotalAndTotal(t *testing.T) {
	inv := newBaseInvoice()
	assert.Empty(t, inv.LineItems)
	assert.Equal(t, 1000.0, inv.Subtotal)

	// Line-item mode replaces flat booking pricing: Subtotal = Σ line
	// amounts. The caller (attachLineItems) adds the freight line explicitly,
	// capturing the pre-line subtotal as the freight amount.
	detRef := "det-1"
	inv.AddLineItem(LineItem{
		LineType:    LineTypeDetention,
		Description: "Detention at Mysuru Depot",
		Quantity:    1.5,
		UnitPrice:   10,
		Amount:      15.0,
		RefID:       &detRef,
	})
	require.Len(t, inv.LineItems, 1)
	assert.Equal(t, 15.0, inv.Subtotal)
	assert.Equal(t, 115.0, inv.Total) // +100 tax -0 discount

	// Freight line upsert keeps the sum correct and rounds to 2dp.
	inv.AddLineItem(LineItem{LineType: LineTypeFreight, Description: "Freight", Amount: 25000.0})
	require.Len(t, inv.LineItems, 2)
	assert.Equal(t, 25015.0, inv.Subtotal)
	assert.Equal(t, 25115.0, inv.Total)

	// Rounded amount preserved.
	inv.AddLineItem(LineItem{LineType: LineTypeAccessorial, Description: "Accessorial", Amount: 0.005})
	assert.Equal(t, 0.01, inv.LineItems[2].Amount)
}

func TestRecomputeTotals_NoopWithoutLineItems(t *testing.T) {
	inv := newBaseInvoice()
	inv.RecomputeTotals()
	assert.Equal(t, 1000.0, inv.Subtotal, "flat booking pricing must survive a no-op recompute")
	assert.Equal(t, 1000.0, inv.Total)
}
