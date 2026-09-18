package converters

import (
	"time"

	db "transport-app/db/generated/sqlite"
	"transport-app/internal/invoice/domain"
	"transport-app/internal/invoice/domain/aggregate"
	"transport-app/internal/shared"
)

// ToDomain converts db.Invoice to *aggregate.InvoiceAggregate.
func ToDomain(i db.Invoice) *aggregate.InvoiceAggregate {
	var tripID *string
	if i.TripID.Valid {
		tripID = &i.TripID.String
	}

	invoiceStatus := aggregate.InvoiceStatus(i.Status)
	if invoiceStatus == "" {
		invoiceStatus = aggregate.InvoiceStatusOutstanding
	}

	var dueDate *time.Time
	if i.DueDate.Valid {
		dueDate = &i.DueDate.Time
	}

	return aggregate.RehydrateInvoiceAggregate(
		aggregate.InvoiceID(i.ID),
		shared.TenantID(i.TenantID),
		i.InvoiceNumber,
		i.BookingID,
		i.CustomerID,
		tripID,
		i.Subtotal,
		i.Tax,
		i.Discount,
		i.Total,
		aggregate.PaymentStatus(i.PaymentStatus),
		invoiceStatus,
		i.PaidAmount,
		0, // no credit column: Rehydrate derives credit from paid/total
		dueDate,
		"",
		"",
		i.CreatedAt,
		i.UpdatedAt,
		i.Version,
	)
}

// ToReadModel converts db.Invoice to domain.InvoiceReadModel.
func ToReadModel(i db.Invoice) domain.InvoiceReadModel {
	var tripID *string
	if i.TripID.Valid {
		tripID = &i.TripID.String
	}

	return domain.InvoiceReadModel{
		ID:            i.ID,
		InvoiceNumber: i.InvoiceNumber,
		BookingID:     i.BookingID,
		CustomerID:    i.CustomerID,
		TripID:        tripID,
		Subtotal:      i.Subtotal,
		Tax:           i.Tax,
		Discount:      i.Discount,
		Total:         i.Total,
		PaymentStatus: i.PaymentStatus,
		CreatedAt:     i.CreatedAt,
		UpdatedAt:     i.UpdatedAt,
	}
}
