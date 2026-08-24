package invoice

import (
	"context"

	"transport-app/internal/domain/types"
)

// InvoiceRepository defines the interface for invoice persistence.
type InvoiceRepository interface {
	CreateInvoice(ctx context.Context, invoice Invoice) (Invoice, error)
	GetInvoiceByID(ctx context.Context, id types.InvoiceID) (InvoiceWithJoins, error)
	GetInvoiceByNumber(ctx context.Context, number string) (InvoiceWithJoins, error)
	GetInvoiceByTripID(ctx context.Context, tripID types.TripID) (InvoiceWithJoins, error)
	GetInvoiceByBookingID(ctx context.Context, bookingID types.BookingID) (InvoiceWithJoins, error)
	UpdateInvoice(ctx context.Context, invoice Invoice) (Invoice, error)
	UpdateInvoicePaymentStatus(ctx context.Context, id types.InvoiceID, status PaymentStatus) (Invoice, error)
	DeleteInvoice(ctx context.Context, id types.InvoiceID) error
	SearchInvoices(ctx context.Context, query string, status string, limit, offset int) ([]InvoiceWithJoins, error)
	ListInvoicesByCustomer(ctx context.Context, customerID types.CustomerID, limit int) ([]InvoiceWithJoins, error)
	CountInvoices(ctx context.Context, query string, status string) (int64, error)
	GetPendingInvoices(ctx context.Context) ([]InvoiceWithJoins, error)
}

// InvoiceWithJoins includes customer, booking, and trip details.
type InvoiceWithJoins struct {
	Invoice
	CustomerName    string
	CustomerCompany *string
	BookingNumber   string
	TripNumber      *string
}
