package aggregate

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"transport-app/internal/shared"
)

func TestNewPaymentAggregate(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	tenantID := shared.TenantID("tenant-1")
	ref := "REF-123"
	remarks := "test payment"
	agg := NewPaymentAggregate(
		"pay-123",
		tenantID,
		"inv-1",
		now,
		500.0,
		PaymentMethodUPI,
		&ref,
		&remarks,
		now,
	)
	require.NotNil(t, agg)
	assert.Equal(t, PaymentID("pay-123"), agg.ID)
	assert.Equal(t, tenantID, agg.TenantID)
	assert.Equal(t, "inv-1", agg.InvoiceID)
	assert.Equal(t, now, agg.PaymentDate)
	assert.Equal(t, 500.0, agg.Amount)
	assert.Equal(t, PaymentMethodUPI, agg.Method)
	require.NotNil(t, agg.Reference)
	assert.Equal(t, "REF-123", *agg.Reference)
	require.NotNil(t, agg.Remarks)
	assert.Equal(t, "test payment", *agg.Remarks)
	assert.Equal(t, now, agg.CreatedAt)
	assert.Equal(t, now, agg.UpdatedAt)
	assert.Len(t, agg.Events(), 1)
	ev, ok := agg.Events()[0].(PaymentReceivedEvent)
	require.True(t, ok)
	assert.Equal(t, PaymentID("pay-123"), ev.ID)
	assert.Equal(t, tenantID, ev.TenantID)
	assert.Equal(t, "inv-1", ev.InvoiceID)
	assert.Equal(t, 500.0, ev.Amount)
	assert.Equal(t, now, ev.CreatedAt)
}

func TestNewPaymentAggregate_NilReferenceRemarks(t *testing.T) {
	now := time.Now()
	agg := NewPaymentAggregate("p1", shared.TenantID("1"), "inv-1", now, 100, PaymentMethodCash, nil, nil, now)
	require.Nil(t, agg.Reference)
	require.Nil(t, agg.Remarks)
	assert.Len(t, agg.Events(), 1)
}

func TestPaymentAggregate_EventsAndClearEvents(t *testing.T) {
	now := time.Now()
	agg := NewPaymentAggregate("p1", "1", "inv-1", now, 100, PaymentMethodCash, nil, nil, now)
	require.Len(t, agg.Events(), 1)
	agg.ClearEvents()
	assert.Len(t, agg.Events(), 0)
	assert.Nil(t, agg.Events())

	// New aggregate after clear still produces event on creation
	agg2 := NewPaymentAggregate("p2", "1", "inv-2", now, 200, PaymentMethodBankTransfer, nil, nil, now)
	require.Len(t, agg2.Events(), 1)
	_, ok := agg2.Events()[0].(PaymentReceivedEvent)
	assert.True(t, ok)
	agg2.ClearEvents()
	assert.Len(t, agg2.Events(), 0)
}

func TestPaymentAggregate_Constants(t *testing.T) {
	assert.Equal(t, PaymentMethod("cash"), PaymentMethodCash)
	assert.Equal(t, PaymentMethod("upi"), PaymentMethodUPI)
	assert.Equal(t, PaymentMethod("bank_transfer"), PaymentMethodBankTransfer)
	assert.Equal(t, PaymentMethod("cheque"), PaymentMethodCheque)
	assert.Equal(t, PaymentMethod("razorpay"), PaymentMethodRazorpay)
}

func TestPaymentAggregate_MethodTypes(t *testing.T) {
	now := time.Now()
	methods := []PaymentMethod{
		PaymentMethodCash,
		PaymentMethodUPI,
		PaymentMethodBankTransfer,
		PaymentMethodCheque,
		PaymentMethodRazorpay,
	}
	for _, m := range methods {
		agg := NewPaymentAggregate(PaymentID("id-"+string(m)), "1", "inv-1", now, 10, m, nil, nil, now)
		assert.Equal(t, m, agg.Method)
		assert.Len(t, agg.Events(), 1)
	}
}

func TestPaymentAggregate_IdempotencyKeyViaReference(t *testing.T) {
	// Verify reference handling indirectly via Save repository path is tested elsewhere,
	// here we just ensure aggregate stores reference as provided.
	now := time.Now()
	refSpaces := "   "
	agg := NewPaymentAggregate("p1", "1", "inv-1", now, 100, PaymentMethodCash, &refSpaces, nil, now)
	require.NotNil(t, agg.Reference)
	assert.Equal(t, "   ", *agg.Reference)
}
