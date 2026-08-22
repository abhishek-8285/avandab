package converters

import (
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	db "transport-app/db/generated/sqlite"
	"transport-app/internal/invoice/domain/aggregate"
	"transport-app/internal/shared"
)

func TestToDomain_WithTripIDAndStatus(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	due := now.Add(30 * 24 * time.Hour)
	inv := db.Invoice{
		ID:            "inv-1",
		InvoiceNumber: "INV-0001",
		BookingID:     "bk-1",
		CustomerID:    "cust-1",
		TripID:        sql.NullString{String: "trip-1", Valid: true},
		Subtotal:      1000,
		Tax:           180,
		Discount:      50,
		Total:         1130,
		PaymentStatus: string(aggregate.PaymentStatusPending),
		TenantID:      "t1",
		CreatedAt:     now,
		UpdatedAt:     now,
		Version:       2,
		PaidAmount:    0,
		Status:        string(aggregate.InvoiceStatusIssued),
		DueDate:       sql.NullTime{Time: due, Valid: true},
		Cgst:          90,
		Sgst:          90,
		Igst:          0,
		Irn:           sql.NullString{String: "irn-123", Valid: true},
		IrnAckNo:      sql.NullString{String: "ack-1", Valid: true},
		IrnAckDate:    sql.NullString{String: "2026-08-20", Valid: true},
		SignedQr:      sql.NullString{String: "qr-data", Valid: true},
		EwbNumber:     sql.NullString{String: "ewb-1", Valid: true},
	}
	agg := ToDomain(inv)
	require.NotNil(t, agg)
	assert.Equal(t, aggregate.InvoiceID("inv-1"), agg.ID)
	assert.Equal(t, shared.TenantID("t1"), agg.TenantID)
	assert.Equal(t, "INV-0001", agg.InvoiceNumber)
	assert.Equal(t, "bk-1", agg.BookingID)
	assert.Equal(t, "cust-1", agg.CustomerID)
	require.NotNil(t, agg.TripID)
	assert.Equal(t, "trip-1", *agg.TripID)
	assert.Equal(t, 1000.0, agg.Subtotal)
	assert.Equal(t, 180.0, agg.Tax)
	assert.Equal(t, 50.0, agg.Discount)
	assert.Equal(t, 1130.0, agg.Total)
	assert.Equal(t, aggregate.PaymentStatusPending, agg.PaymentStatus)
	assert.Equal(t, aggregate.InvoiceStatusIssued, agg.Status)
	assert.Equal(t, int64(2), agg.Version)
	require.NotNil(t, agg.DueDate)
	assert.WithinDuration(t, due, *agg.DueDate, time.Second)
	assert.Equal(t, now, agg.CreatedAt)
	assert.Equal(t, now, agg.UpdatedAt)
	// Note: ToDomain via Rehydrate sets FinancialYear/Remarks empty, CreditBalance 0, and GST fields not persisted via Rehydrate in converter (only via repo). But ToDomain currently only maps core fields + dueDate/status/version, not GST/IRN. That is expected.
}

func TestToDomain_NilTripAndEmptyStatusDefaults(t *testing.T) {
	now := time.Now()
	inv := db.Invoice{
		ID:            "inv-2",
		InvoiceNumber: "INV-0002",
		BookingID:     "bk-2",
		CustomerID:    "cust-2",
		TripID:        sql.NullString{Valid: false},
		Subtotal:      500,
		Tax:           0,
		Discount:      0,
		Total:         500,
		PaymentStatus: string(aggregate.PaymentStatusPaid),
		TenantID:      "t1",
		CreatedAt:     now,
		UpdatedAt:     now,
		Version:       1,
		PaidAmount:    500,
		Status:        "", // empty should default to outstanding
		DueDate:       sql.NullTime{Valid: false},
	}
	agg := ToDomain(inv)
	require.NotNil(t, agg)
	assert.Nil(t, agg.TripID)
	assert.Nil(t, agg.DueDate)
	assert.Equal(t, aggregate.InvoiceStatusOutstanding, agg.Status)
	assert.Equal(t, shared.TenantID("t1"), agg.TenantID)
}

func TestToDomain_StatusPreserved(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		status aggregate.InvoiceStatus
	}{
		{aggregate.InvoiceStatusDraft},
		{aggregate.InvoiceStatusPaid},
		{aggregate.InvoiceStatusCancelled},
		{aggregate.InvoiceStatusOutstanding},
	} {
		inv := db.Invoice{
			ID:            "inv-x",
			InvoiceNumber: "INV-X",
			BookingID:     "bk-1",
			CustomerID:    "cust-1",
			Subtotal:      100,
			Tax:           0,
			Total:         100,
			PaymentStatus: "pending",
			TenantID:      "t1",
			CreatedAt:     now,
			UpdatedAt:     now,
			Version:       1,
			Status:        string(tc.status),
		}
		agg := ToDomain(inv)
		assert.Equal(t, tc.status, agg.Status)
	}
}

func TestToReadModel_WithTripID(t *testing.T) {
	now := time.Now()
	inv := db.Invoice{
		ID:            "inv-3",
		InvoiceNumber: "INV-0003",
		BookingID:     "bk-3",
		CustomerID:    "cust-3",
		TripID:        sql.NullString{String: "trip-99", Valid: true},
		Subtotal:      2000,
		Tax:           100,
		Discount:      0,
		Total:         2100,
		PaymentStatus: "pending",
		TenantID:      "t1",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	rm := ToReadModel(inv)
	assert.Equal(t, "inv-3", rm.ID)
	assert.Equal(t, "INV-0003", rm.InvoiceNumber)
	assert.Equal(t, "bk-3", rm.BookingID)
	assert.Equal(t, "cust-3", rm.CustomerID)
	require.NotNil(t, rm.TripID)
	assert.Equal(t, "trip-99", *rm.TripID)
	assert.Equal(t, 2000.0, rm.Subtotal)
	assert.Equal(t, 100.0, rm.Tax)
	assert.Equal(t, 2100.0, rm.Total)
	assert.Equal(t, "pending", rm.PaymentStatus)
	assert.Equal(t, now, rm.CreatedAt)
}

func TestToReadModel_NilTripID(t *testing.T) {
	now := time.Now()
	inv := db.Invoice{
		ID:            "inv-4",
		InvoiceNumber: "INV-0004",
		BookingID:     "bk-4",
		CustomerID:    "cust-4",
		TripID:        sql.NullString{Valid: false},
		Subtotal:      300,
		Tax:           0,
		Total:         300,
		PaymentStatus: "paid",
		TenantID:      "t1",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	rm := ToReadModel(inv)
	assert.Nil(t, rm.TripID)
	assert.Equal(t, "inv-4", rm.ID)
	assert.Equal(t, 300.0, rm.Total)
}

func TestToReadModel_ZeroValues(t *testing.T) {
	inv := db.Invoice{
		ID:            "inv-0",
		InvoiceNumber: "INV-0",
		BookingID:     "bk-0",
		CustomerID:    "cust-0",
		TenantID:      "t0",
		PaymentStatus: "pending",
	}
	rm := ToReadModel(inv)
	assert.Equal(t, "inv-0", rm.ID)
	assert.Nil(t, rm.TripID)
	assert.Equal(t, 0.0, rm.Subtotal)
}
