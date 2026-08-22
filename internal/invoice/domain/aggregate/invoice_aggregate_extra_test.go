package aggregate

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"transport-app/internal/shared"
)

func TestUpdatePaymentStatus_Success(t *testing.T) {
	now := time.Now()
	tripID := "trip-1"
	inv := NewInvoiceAggregate(InvoiceID("inv-upd"), shared.TenantID("t1"), "INV-UPD-01", "bk-1", "cust-1", &tripID, 1000, 100, 0, 1100, PaymentStatusPending, now)
	inv.ClearEvents()
	later := now.Add(time.Hour)
	err := inv.UpdatePaymentStatus(PaymentStatusPaid, later)
	require.NoError(t, err)
	assert.Equal(t, PaymentStatusPaid, inv.PaymentStatus)
	assert.Equal(t, later, inv.UpdatedAt)
	require.Len(t, inv.Events(), 1)
	ev, ok := inv.Events()[0].(InvoicePaymentUpdatedEvent)
	require.True(t, ok)
	assert.Equal(t, InvoiceID("inv-upd"), ev.ID)
	assert.Equal(t, PaymentStatusPaid, ev.PaymentStatus)
	assert.Equal(t, later, ev.UpdatedAt)
}

func TestUpdatePaymentStatus_MultipleTransitions(t *testing.T) {
	now := time.Now()
	inv := NewInvoiceAggregate(InvoiceID("inv-upd2"), shared.TenantID("t1"), "INV-UPD-02", "bk-1", "cust-1", nil, 500, 0, 0, 500, PaymentStatusPending, now)
	inv.ClearEvents()
	for _, status := range []PaymentStatus{PaymentStatusPartiallyPaid, PaymentStatusPaid, PaymentStatusPending} {
		ts := time.Now()
		require.NoError(t, inv.UpdatePaymentStatus(status, ts))
		assert.Equal(t, status, inv.PaymentStatus)
		assert.Equal(t, ts, inv.UpdatedAt)
	}
	assert.Len(t, inv.Events(), 3)
}

func TestRecomputeTotals_WithLineTaxes(t *testing.T) {
	now := time.Now()
	inv := NewInvoiceAggregate(InvoiceID("inv-tax"), shared.TenantID("t1"), "INV-TAX-01", "bk-1", "cust-1", nil, 1000, 100, 10, 1090, PaymentStatusPending, now)
	inv.Discount = 10
	// add first line with CGST/SGST
	inv.AddLineItem(LineItem{
		LineType:    LineTypeFreight,
		Description: "Freight",
		Quantity:    1,
		UnitPrice:   1000,
		Amount:      1000,
		CgstAmount:  90,
		SgstAmount:  90,
		IgstAmount:  0,
	})
	// subtotal 1000, cgst 90 sgst 90 igst 0 tax 180 total = 1000+180-10=1170
	assert.Equal(t, 1000.0, inv.Subtotal)
	assert.Equal(t, 90.0, inv.Cgst)
	assert.Equal(t, 90.0, inv.Sgst)
	assert.Equal(t, 0.0, inv.Igst)
	assert.Equal(t, 180.0, inv.Tax)
	assert.Equal(t, 1170.0, inv.Total)

	// second line with IGST
	inv.AddLineItem(LineItem{
		LineType:    LineTypeDetention,
		Description: "Detention",
		Quantity:    2,
		UnitPrice:   100,
		Amount:      200,
		CgstAmount:  0,
		SgstAmount:  0,
		IgstAmount:  36,
	})
	// subtotal 1200, cgst 90 sgst 90 igst 36 tax 216 total 1406
	assert.Equal(t, 1200.0, inv.Subtotal)
	assert.Equal(t, 90.0, inv.Cgst)
	assert.Equal(t, 90.0, inv.Sgst)
	assert.Equal(t, 36.0, inv.Igst)
	assert.Equal(t, 216.0, inv.Tax)
	assert.Equal(t, 1406.0, inv.Total)
}

func TestRecomputeTotals_WithoutLineTaxes_PreservesTax(t *testing.T) {
	now := time.Now()
	inv := NewInvoiceAggregate(InvoiceID("inv-notax"), shared.TenantID("t1"), "INV-NOTAX", "bk-1", "cust-1", nil, 5000, 250, 0, 5250, PaymentStatusPending, now)
	// Tax 250 from flat pricing should be preserved when lines have no taxes
	inv.AddLineItem(LineItem{
		LineType:    LineTypeFreight,
		Description: "Freight",
		Amount:      5000,
	})
	assert.Equal(t, 5000.0, inv.Subtotal)
	// hasLineTaxes false => Cgst/Sgst/Igst remain 0, Tax stays 250? Check implementation: if hasLineTaxes false, Tax not updated, remains original 250
	assert.Equal(t, 250.0, inv.Tax)
	assert.Equal(t, 5250.0, inv.Total)

	inv.AddLineItem(LineItem{
		LineType:    LineTypeAccessorial,
		Description: "Extra",
		Amount:      100,
	})
	assert.Equal(t, 5100.0, inv.Subtotal)
	assert.Equal(t, 250.0, inv.Tax)
	assert.Equal(t, 5350.0, inv.Total)
}

func TestRecomputeTotals_DiscountApplied(t *testing.T) {
	now := time.Now()
	inv := NewInvoiceAggregate(InvoiceID("inv-disc"), shared.TenantID("t1"), "INV-DISC", "bk-1", "cust-1", nil, 1000, 0, 100, 900, PaymentStatusPending, now)
	inv.Discount = 100
	inv.AddLineItem(LineItem{
		LineType:    LineTypeFreight,
		Description: "Freight",
		Amount:      1000,
		CgstAmount:  50,
		SgstAmount:  50,
	})
	// subtotal 1000, tax 100, discount 100 => total 1000
	assert.Equal(t, 1000.0, inv.Subtotal)
	assert.Equal(t, 100.0, inv.Tax)
	assert.Equal(t, 1000.0, inv.Total)
}

func TestRecomputeTotals_Rounding(t *testing.T) {
	now := time.Now()
	inv := NewInvoiceAggregate(InvoiceID("inv-round"), shared.TenantID("t1"), "INV-ROUND", "bk-1", "cust-1", nil, 0, 0, 0, 0, PaymentStatusPending, now)
	inv.Discount = 0.005 // tiny discount to test rounding? Actually discount not recomputed via lines except via Total formula
	inv.AddLineItem(LineItem{
		LineType:    LineTypeFreight,
		Description: "Freight",
		Amount:      100.005, // will be rounded to 100.01 via AddLineItem
		CgstAmount:  9.004,   // not rounded individually until sum? Implementation sums then rounds
		SgstAmount:  9.004,
	})
	// After AddLineItem: Amount rounded to 100.01, subtotal rounded to 100.01, cgst 9.00? 9.004 rounded? Actually RoundMoney(9.004) = 9.0? No, sum then Round: cgst = RoundMoney(9.004)=9.00, sgst same, tax 18.00, total = Round(100.01+18-0.005)=118.005 -> 118.01? Wait discount 0.005 remains? Let's compute via code: Total = Round(subtotal+tax-discount) -> 100.01+18=118.01-0.005=118.005 -> Round=118.01
	assert.Equal(t, 100.01, inv.LineItems[0].Amount)
	assert.Equal(t, 100.01, inv.Subtotal)
	// cgst/sgst rounding: 9.004 -> RoundMoney => 9.0
	assert.Equal(t, 9.0, inv.Cgst)
	assert.Equal(t, 9.0, inv.Sgst)
	assert.Equal(t, 18.0, inv.Tax)
}

func TestRoundMoney_EdgeCases(t *testing.T) {
	cases := []struct {
		in   float64
		want float64
	}{
		{100.004, 100.00},
		{100.005, 100.01},
		{100.015, 100.02},
		{0.005, 0.01},
		{0.004, 0.00},
		{123456.789, 123456.79},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, RoundMoney(tc.in), "RoundMoney(%v)", tc.in)
	}
}

func TestApplyPayment_ZeroAmountPending(t *testing.T) {
	now := time.Now()
	inv := NewInvoiceAggregate(InvoiceID("inv-zero"), shared.TenantID("t1"), "INV-ZERO", "bk-1", "cust-1", nil, 1000, 0, 0, 1000, PaymentStatusPending, now)
	inv.ClearEvents()
	inv.Status = InvoiceStatusOutstanding
	inv.PaidAmount = 0
	inv.PaymentStatus = PaymentStatusPending
	err := inv.ApplyPayment(0, now.Add(time.Hour))
	require.NoError(t, err)
	// PaidAmount stays 0, outstanding 1000 >0, PaidAmount==0 => pending
	assert.Equal(t, 0.0, inv.PaidAmount)
	assert.Equal(t, PaymentStatusPending, inv.PaymentStatus)
	assert.NotEqual(t, InvoiceStatusPaid, inv.Status)
	require.Len(t, inv.Events(), 1)
}

func TestAddLineItem_MultipleAndNoop(t *testing.T) {
	now := time.Now()
	inv := NewInvoiceAggregate(InvoiceID("inv-multi"), shared.TenantID("t1"), "INV-MULTI", "bk-1", "cust-1", nil, 2000, 100, 0, 2100, PaymentStatusPending, now)
	// No line items => RecomputeTotals noop preserves flat pricing
	inv.RecomputeTotals()
	assert.Equal(t, 2000.0, inv.Subtotal)
	assert.Equal(t, 2100.0, inv.Total)

	// Now add lines: first freight line at 2000 amount will replace flat subtotal
	inv.AddLineItem(LineItem{LineType: LineTypeFreight, Description: "Freight", Amount: 2000})
	assert.Equal(t, 2000.0, inv.Subtotal)
	inv.AddLineItem(LineItem{LineType: LineTypeDetention, Description: "Detention", Amount: 500.555}) // rounded 500.56
	assert.Equal(t, 500.56, inv.LineItems[1].Amount)
	assert.Equal(t, 2500.56, inv.Subtotal)
	// Tax still 100 since no line taxes, total 2600.56
	assert.Equal(t, 2600.56, inv.Total)
}

func TestInvoiceAggregate_EventsLifecycle(t *testing.T) {
	now := time.Now()
	inv := NewInvoiceAggregate(InvoiceID("inv-ev"), shared.TenantID("t1"), "INV-EV", "bk-1", "cust-1", nil, 100, 0, 0, 100, PaymentStatusPending, now)
	assert.Len(t, inv.Events(), 1)
	inv.ClearEvents()
	assert.Empty(t, inv.Events())
	require.NoError(t, inv.UpdatePaymentStatus(PaymentStatusPaid, now))
	assert.Len(t, inv.Events(), 1)
	inv.ClearEvents()
	assert.Empty(t, inv.Events())
}
