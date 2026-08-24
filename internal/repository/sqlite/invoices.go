package sqlite

import (
	"context"
	"database/sql"

	"transport-app/internal/domain"
	"transport-app/internal/repository"

	db "transport-app/db/generated/sqlite"

	"transport-app/internal/shared"
)

// InvoiceRepository implementation

func (r *SQLRepository) CreateInvoice(ctx context.Context, invoice domain.Invoice) (domain.Invoice, error) {
	var tripID sql.NullString
	if invoice.TripID != nil {
		tripID = sql.NullString{String: string(*invoice.TripID), Valid: true}
	}

	created, err := r.Q(ctx).CreateInvoice(ctx, db.CreateInvoiceParams{
		ID:            string(invoice.ID),
		InvoiceNumber: invoice.InvoiceNumber,
		BookingID:     string(invoice.BookingID),
		CustomerID:    string(invoice.CustomerID),
		TripID:        tripID,
		Subtotal:      invoice.Subtotal,
		Tax:           invoice.Tax,
		Discount:      invoice.Discount,
		Total:         invoice.Total,
		PaymentStatus: string(invoice.PaymentStatus),
		TenantID:      string(shared.TenantIDFromContext(ctx)),
	})
	if err != nil {
		return domain.Invoice{}, err
	}
	v := db.Invoice{
		ID:            created.ID,
		InvoiceNumber: created.InvoiceNumber,
		BookingID:     created.BookingID,
		CustomerID:    created.CustomerID,
		TripID:        created.TripID,
		Subtotal:      created.Subtotal,
		Tax:           created.Tax,
		Discount:      created.Discount,
		Total:         created.Total,
		PaymentStatus: created.PaymentStatus,
		CreatedAt:     created.CreatedAt,
		UpdatedAt:     created.UpdatedAt,
		TenantID:      created.TenantID,
	}
	return toDomainInvoice(v), nil
}

func (r *SQLRepository) GetInvoiceByID(ctx context.Context, id domain.InvoiceID) (repository.InvoiceWithJoins, error) {
	row, err := r.Q(ctx).GetInvoiceByID(ctx, db.GetInvoiceByIDParams{
		ID:       string(id),
		TenantID: string(shared.TenantIDFromContext(ctx)),
	})
	if err != nil {
		return repository.InvoiceWithJoins{}, err
	}
	return invoiceRowToWithJoins(
		row.ID, row.InvoiceNumber, row.BookingID, row.CustomerID,
		row.TripID, row.Subtotal, row.Tax, row.Discount, row.Total,
		row.PaymentStatus, row.CreatedAt, row.UpdatedAt,
		row.CustomerName, row.CustomerCompany, row.BookingNumber, row.TripNumber,
	), nil
}

func (r *SQLRepository) GetInvoiceByNumber(ctx context.Context, number string) (repository.InvoiceWithJoins, error) {
	row, err := r.Q(ctx).GetInvoiceByNumber(ctx, db.GetInvoiceByNumberParams{
		InvoiceNumber: number,
		TenantID:      string(shared.TenantIDFromContext(ctx)),
	})
	if err != nil {
		return repository.InvoiceWithJoins{}, err
	}
	return invoiceRowToWithJoins(
		row.ID, row.InvoiceNumber, row.BookingID, row.CustomerID,
		row.TripID, row.Subtotal, row.Tax, row.Discount, row.Total,
		row.PaymentStatus, row.CreatedAt, row.UpdatedAt,
		row.CustomerName, row.CustomerCompany, row.BookingNumber, row.TripNumber,
	), nil
}

func (r *SQLRepository) GetInvoiceByTripID(ctx context.Context, tripID domain.TripID) (repository.InvoiceWithJoins, error) {
	row, err := r.Q(ctx).GetInvoiceByTripID(ctx, db.GetInvoiceByTripIDParams{
		TripID:   sql.NullString{String: string(tripID), Valid: true},
		TenantID: string(shared.TenantIDFromContext(ctx)),
	})
	if err != nil {
		return repository.InvoiceWithJoins{}, err
	}
	return invoiceRowToWithJoins(
		row.ID, row.InvoiceNumber, row.BookingID, row.CustomerID,
		row.TripID, row.Subtotal, row.Tax, row.Discount, row.Total,
		row.PaymentStatus, row.CreatedAt, row.UpdatedAt,
		row.CustomerName, row.CustomerCompany, row.BookingNumber, row.TripNumber,
	), nil
}

func (r *SQLRepository) GetInvoiceByBookingID(ctx context.Context, bookingID domain.BookingID) (repository.InvoiceWithJoins, error) {
	row, err := r.Q(ctx).GetInvoiceByBookingID(ctx, db.GetInvoiceByBookingIDParams{
		BookingID: string(bookingID),
		TenantID:  string(shared.TenantIDFromContext(ctx)),
	})
	if err != nil {
		return repository.InvoiceWithJoins{}, err
	}
	return invoiceRowToWithJoins(
		row.ID, row.InvoiceNumber, row.BookingID, row.CustomerID,
		row.TripID, row.Subtotal, row.Tax, row.Discount, row.Total,
		row.PaymentStatus, row.CreatedAt, row.UpdatedAt,
		row.CustomerName, row.CustomerCompany, row.BookingNumber, row.TripNumber,
	), nil
}

func (r *SQLRepository) UpdateInvoice(ctx context.Context, invoice domain.Invoice) (domain.Invoice, error) {
	var tripID sql.NullString
	if invoice.TripID != nil {
		tripID = sql.NullString{String: string(*invoice.TripID), Valid: true}
	}

	updated, err := r.Q(ctx).UpdateInvoice(ctx, db.UpdateInvoiceParams{
		InvoiceNumber: invoice.InvoiceNumber,
		BookingID:     string(invoice.BookingID),
		CustomerID:    string(invoice.CustomerID),
		TripID:        tripID,
		Subtotal:      invoice.Subtotal,
		Tax:           invoice.Tax,
		Discount:      invoice.Discount,
		Total:         invoice.Total,
		PaymentStatus: string(invoice.PaymentStatus),
		ID:            string(invoice.ID),
		TenantID:      string(shared.TenantIDFromContext(ctx)),
	})
	if err != nil {
		return domain.Invoice{}, err
	}
	v := db.Invoice{
		ID:            updated.ID,
		InvoiceNumber: updated.InvoiceNumber,
		BookingID:     updated.BookingID,
		CustomerID:    updated.CustomerID,
		TripID:        updated.TripID,
		Subtotal:      updated.Subtotal,
		Tax:           updated.Tax,
		Discount:      updated.Discount,
		Total:         updated.Total,
		PaymentStatus: updated.PaymentStatus,
		CreatedAt:     updated.CreatedAt,
		UpdatedAt:     updated.UpdatedAt,
		TenantID:      updated.TenantID,
	}
	return toDomainInvoice(v), nil
}

func (r *SQLRepository) UpdateInvoicePaymentStatus(ctx context.Context, id domain.InvoiceID, status domain.PaymentStatus) (domain.Invoice, error) {
	updated, err := r.Q(ctx).UpdateInvoicePaymentStatus(ctx, db.UpdateInvoicePaymentStatusParams{
		PaymentStatus: string(status),
		ID:            string(id),
		TenantID:      string(shared.TenantIDFromContext(ctx)),
	})
	if err != nil {
		return domain.Invoice{}, err
	}
	v := db.Invoice{
		ID:            updated.ID,
		InvoiceNumber: updated.InvoiceNumber,
		BookingID:     updated.BookingID,
		CustomerID:    updated.CustomerID,
		TripID:        updated.TripID,
		Subtotal:      updated.Subtotal,
		Tax:           updated.Tax,
		Discount:      updated.Discount,
		Total:         updated.Total,
		PaymentStatus: updated.PaymentStatus,
		CreatedAt:     updated.CreatedAt,
		UpdatedAt:     updated.UpdatedAt,
		TenantID:      updated.TenantID,
	}
	return toDomainInvoice(v), nil
}

func (r *SQLRepository) DeleteInvoice(ctx context.Context, id domain.InvoiceID) error {
	return r.Q(ctx).DeleteInvoice(ctx, db.DeleteInvoiceParams{
		ID:       string(id),
		TenantID: string(shared.TenantIDFromContext(ctx)),
	})
}

func (r *SQLRepository) ListInvoicesByCustomer(ctx context.Context, customerID domain.CustomerID, limit int) ([]repository.InvoiceWithJoins, error) {
	rows, err := r.Q(ctx).ListInvoicesByCustomer(ctx, db.ListInvoicesByCustomerParams{
		TenantID:   string(shared.TenantIDFromContext(ctx)),
		CustomerID: string(customerID),
		Limit:      int64(limit),
	})
	if err != nil {
		return nil, err
	}
	result := make([]repository.InvoiceWithJoins, len(rows))
	for i, row := range rows {
		result[i] = invoiceRowToWithJoins(
			row.ID, row.InvoiceNumber, row.BookingID, row.CustomerID,
			row.TripID, row.Subtotal, row.Tax, row.Discount, row.Total,
			row.PaymentStatus, row.CreatedAt, row.UpdatedAt,
			row.CustomerName, row.CustomerCompany, row.BookingNumber, row.TripNumber,
		)
	}
	return result, nil
}

func (r *SQLRepository) SearchInvoices(ctx context.Context, query string, status string, limit, offset int) ([]repository.InvoiceWithJoins, error) {
	rows, err := r.Q(ctx).SearchInvoices(ctx, db.SearchInvoicesParams{
		TenantID:      string(shared.TenantIDFromContext(ctx)),
		Column2:       sql.NullString{String: query, Valid: true},
		Column3:       sql.NullString{String: query, Valid: true},
		Column4:       status,
		PaymentStatus: status,
		Limit:         int64(limit),
		Offset:        int64(offset),
	})
	if err != nil {
		return nil, err
	}
	result := make([]repository.InvoiceWithJoins, len(rows))
	for i, row := range rows {
		result[i] = invoiceRowToWithJoins(
			row.ID, row.InvoiceNumber, row.BookingID, row.CustomerID,
			row.TripID, row.Subtotal, row.Tax, row.Discount, row.Total,
			row.PaymentStatus, row.CreatedAt, row.UpdatedAt,
			row.CustomerName, row.CustomerCompany, row.BookingNumber, row.TripNumber,
		)
	}
	return result, nil
}

func (r *SQLRepository) CountInvoices(ctx context.Context, query string, status string) (int64, error) {
	count, err := r.Q(ctx).CountInvoices(ctx, db.CountInvoicesParams{
		TenantID:      string(shared.TenantIDFromContext(ctx)),
		Column2:       sql.NullString{String: query, Valid: true},
		Column3:       sql.NullString{String: query, Valid: true},
		Column4:       status,
		PaymentStatus: status,
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (r *SQLRepository) GetPendingInvoices(ctx context.Context) ([]repository.InvoiceWithJoins, error) {
	rows, err := r.Q(ctx).GetPendingInvoices(ctx, string(shared.TenantIDFromContext(ctx)))
	if err != nil {
		return nil, err
	}
	result := make([]repository.InvoiceWithJoins, len(rows))
	for i, row := range rows {
		result[i] = invoiceRowToWithJoins(
			row.ID, row.InvoiceNumber, row.BookingID, row.CustomerID,
			row.TripID, row.Subtotal, row.Tax, row.Discount, row.Total,
			row.PaymentStatus, row.CreatedAt, row.UpdatedAt,
			row.CustomerName, row.CustomerCompany, row.BookingNumber, row.TripNumber,
		)
	}
	return result, nil
}
