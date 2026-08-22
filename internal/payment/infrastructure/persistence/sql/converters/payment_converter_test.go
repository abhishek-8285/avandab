package converters

import (
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "transport-app/db/generated/sqlite"
	"transport-app/internal/payment/domain/aggregate"
	"transport-app/internal/shared"
)

func TestGetStringPointer_NilAndValid(t *testing.T) {
	valid := sql.NullString{String: "hello", Valid: true}
	invalid := sql.NullString{Valid: false}

	ptr := getStringPointer(valid)
	require.NotNil(t, ptr)
	assert.Equal(t, "hello", *ptr)

	ptr = getStringPointer(invalid)
	assert.Nil(t, ptr)

	// Empty string but valid
	emptyValid := sql.NullString{String: "", Valid: true}
	ptr = getStringPointer(emptyValid)
	require.NotNil(t, ptr)
	assert.Equal(t, "", *ptr)
}

func TestToDomain_MapsAllFields(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	ref := sql.NullString{String: "REF-001", Valid: true}
	rem := sql.NullString{String: "remarks", Valid: true}
	p := db.Payment{
		ID:          "pay-123",
		InvoiceID:   "inv-456",
		PaymentDate: now,
		Amount:      1234.56,
		Method:      string(aggregate.PaymentMethodUPI),
		Reference:   ref,
		Remarks:     rem,
		TenantID:    "tenant-1",
		CreatedAt:   now,
		UpdatedAt:   now.Add(time.Hour),
	}
	agg := ToDomain(p)
	require.NotNil(t, agg)
	assert.Equal(t, aggregate.PaymentID("pay-123"), agg.ID)
	assert.Equal(t, shared.TenantID("tenant-1"), agg.TenantID)
	assert.Equal(t, "inv-456", agg.InvoiceID)
	assert.Equal(t, now, agg.PaymentDate)
	assert.Equal(t, 1234.56, agg.Amount)
	assert.Equal(t, aggregate.PaymentMethodUPI, agg.Method)
	require.NotNil(t, agg.Reference)
	assert.Equal(t, "REF-001", *agg.Reference)
	require.NotNil(t, agg.Remarks)
	assert.Equal(t, "remarks", *agg.Remarks)
	// ToDomain uses CreatedAt for both CreatedAt and UpdatedAt via NewPaymentAggregate
	assert.Equal(t, now, agg.CreatedAt)
	assert.Len(t, agg.Events(), 1)
	_, ok := agg.Events()[0].(aggregate.PaymentReceivedEvent)
	assert.True(t, ok)
	agg.ClearEvents()
	assert.Len(t, agg.Events(), 0)
}

func TestToDomain_NilReferenceRemarks(t *testing.T) {
	now := time.Now()
	p := db.Payment{
		ID:          "pay-nil",
		InvoiceID:   "inv-1",
		PaymentDate: now,
		Amount:      100,
		Method:      string(aggregate.PaymentMethodCash),
		Reference:   sql.NullString{Valid: false},
		Remarks:     sql.NullString{Valid: false},
		TenantID:    "1",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	agg := ToDomain(p)
	assert.Nil(t, agg.Reference)
	assert.Nil(t, agg.Remarks)
	assert.Equal(t, aggregate.PaymentID("pay-nil"), agg.ID)
}

func TestToDomain_AllMethods(t *testing.T) {
	now := time.Now()
	methods := []aggregate.PaymentMethod{
		aggregate.PaymentMethodCash,
		aggregate.PaymentMethodUPI,
		aggregate.PaymentMethodBankTransfer,
		aggregate.PaymentMethodCheque,
		aggregate.PaymentMethodRazorpay,
	}
	for _, m := range methods {
		p := db.Payment{
			ID:          "pay-" + string(m),
			InvoiceID:   "inv-1",
			PaymentDate: now,
			Amount:      10,
			Method:      string(m),
			TenantID:    "1",
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		agg := ToDomain(p)
		assert.Equal(t, m, agg.Method)
	}
}

func TestToReadModel_MapsAllFields(t *testing.T) {
	now := time.Date(2026, 8, 20, 15, 0, 0, 0, time.UTC)
	ref := sql.NullString{String: "REF-RM", Valid: true}
	rem := sql.NullString{String: "note", Valid: true}
	p := db.Payment{
		ID:          "pay-rm-1",
		InvoiceID:   "inv-789",
		PaymentDate: now,
		Amount:      999.99,
		Method:      string(aggregate.PaymentMethodBankTransfer),
		Reference:   ref,
		Remarks:     rem,
		TenantID:    "t1",
		CreatedAt:   now,
		UpdatedAt:   now.Add(2 * time.Hour),
	}
	rm := ToReadModel(p)
	assert.Equal(t, "pay-rm-1", rm.ID)
	assert.Equal(t, "inv-789", rm.InvoiceID)
	assert.Equal(t, now, rm.PaymentDate)
	assert.Equal(t, 999.99, rm.Amount)
	assert.Equal(t, string(aggregate.PaymentMethodBankTransfer), rm.Method)
	require.NotNil(t, rm.Reference)
	assert.Equal(t, "REF-RM", *rm.Reference)
	require.NotNil(t, rm.Remarks)
	assert.Equal(t, "note", *rm.Remarks)
	assert.Equal(t, now, rm.CreatedAt)
	assert.Equal(t, now.Add(2*time.Hour), rm.UpdatedAt)
}

func TestToReadModel_NilFields(t *testing.T) {
	now := time.Now()
	p := db.Payment{
		ID:          "pay-nil",
		InvoiceID:   "inv-1",
		PaymentDate: now,
		Amount:      50,
		Method:      "cash",
		Reference:   sql.NullString{Valid: false},
		Remarks:     sql.NullString{Valid: false},
		TenantID:    "1",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	rm := ToReadModel(p)
	assert.Nil(t, rm.Reference)
	assert.Nil(t, rm.Remarks)
	assert.Equal(t, "pay-nil", rm.ID)
}

func TestToReadModel_ZeroAmountValid(t *testing.T) {
	now := time.Now()
	p := db.Payment{
		ID:          "pay-zero",
		InvoiceID:   "inv-1",
		PaymentDate: now,
		Amount:      0,
		Method:      "cash",
		Reference:   sql.NullString{Valid: false},
		Remarks:     sql.NullString{Valid: false},
		TenantID:    "1",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	rm := ToReadModel(p)
	assert.Equal(t, 0.0, rm.Amount)
	agg := ToDomain(p)
	assert.Equal(t, 0.0, agg.Amount)
}
