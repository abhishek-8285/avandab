package domain

import (
	"context"
	"time"

	"transport-app/internal/invoice/domain/aggregate"
	"transport-app/internal/shared"
)

// InvoiceReadModel optimized for read operations.
type InvoiceReadModel struct {
	ID              string
	InvoiceNumber   string
	BookingID       string
	BookingNumber   string
	CustomerID      string
	CustomerName    string
	CustomerCompany string
	TripID          *string
	TripNumber      string
	Subtotal        float64
	Tax             float64
	Discount        float64
	Total           float64
	PaymentStatus   string
	CGST            float64
	SGST            float64
	IGST            float64
	IRN             string
	IRNAckNo        string
	IRNAckDate      string
	// IRNCancelledAt is the raw timestamp from invoices.irn_cancelled_at
	// (migration 00099); empty string when the IRN was never cancelled.
	IRNCancelledAt string
	SignedQR       string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// InvoiceRepository defines the persistence contract for invoices.
type InvoiceRepository interface {
	Save(ctx context.Context, inv *aggregate.InvoiceAggregate) error
	Find(ctx context.Context, id aggregate.InvoiceID, tenantID shared.TenantID) (*aggregate.InvoiceAggregate, error)
	FindByBookingID(ctx context.Context, bookingID string, tenantID shared.TenantID) (*aggregate.InvoiceAggregate, error)
	// FindByTripID resolves the invoice for a trip (detention billing for
	// trips without bookings, Spec 02 §6).
	FindByTripID(ctx context.Context, tripID string, tenantID shared.TenantID) (*aggregate.InvoiceAggregate, error)
	GetReadModel(ctx context.Context, id aggregate.InvoiceID, tenantID shared.TenantID) (InvoiceReadModel, error)
	SearchReadModels(ctx context.Context, tenantID shared.TenantID, query string, status string, limit int, offset int) ([]InvoiceReadModel, int64, error)
}
