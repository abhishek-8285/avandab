package sql

import (
	"context"
	"database/sql"

	"transport-app/internal/invoice/domain"
	"transport-app/internal/shared"
)

// Date-range search variant (optional interface asserted by ListInvoicesUseCase
// when from/to filters are present). Raw SQL keeps the sqlc query set and
// existing mocks untouched (same coexistence pattern as
// booking_repository_daterange.go).
//
// The invoices table has no dedicated invoice_date column; the invoice date is
// created_at (the PDF and invoice view render CreatedAt as the invoice date).
// Timestamps are stored RFC3339 ("2026-08-15T08:00:00Z"), which SQLite date()
// cannot parse directly — hence date(substr(col,1,10)).

const invoiceDateClause = `
  AND (? = '' OR date(substr(i.created_at,1,10)) >= date(?))
  AND (? = '' OR date(substr(i.created_at,1,10)) <= date(?))`

const invoiceDateRangeSelect = `
SELECT i.id, i.invoice_number, i.booking_id, i.customer_id, i.trip_id,
    i.subtotal, i.tax, i.discount, i.total, i.payment_status, i.tenant_id, i.created_at, i.updated_at,
    c.name AS customer_name, c.company AS customer_company, b.booking_number, t.trip_number
FROM invoices i
JOIN customers c ON i.customer_id = c.id
LEFT JOIN bookings b ON i.booking_id = b.id
LEFT JOIN trips t ON i.trip_id = t.id
WHERE i.tenant_id = ?
  AND (i.invoice_number LIKE '%' || ? || '%' OR c.name LIKE '%' || ? || '%')
  AND (? = '' OR i.payment_status = ?)`

const invoiceDateRangeCount = `
SELECT COUNT(*)
FROM invoices i
JOIN customers c ON i.customer_id = c.id
LEFT JOIN bookings b ON i.booking_id = b.id
LEFT JOIN trips t ON i.trip_id = t.id
WHERE i.tenant_id = ?
  AND (i.invoice_number LIKE '%' || ? || '%' OR c.name LIKE '%' || ? || '%')
  AND (? = '' OR i.payment_status = ?)`

func (r *invoiceRepository) SearchReadModelsDateRange(ctx context.Context, tenantID shared.TenantID, query string, status string, from string, to string, limit int, offset int) ([]domain.InvoiceReadModel, int64, error) {
	rows, err := r.exec(ctx).QueryContext(ctx,
		invoiceDateRangeSelect+invoiceDateClause+`
ORDER BY i.created_at DESC
LIMIT ? OFFSET ?`,
		string(tenantID), query, query, status, status, from, from, to, to, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	readModels, err := scanInvoiceReadModels(rows)
	if err != nil {
		return nil, 0, err
	}

	var count int64
	if err := r.exec(ctx).QueryRowContext(ctx, invoiceDateRangeCount+invoiceDateClause,
		string(tenantID), query, query, status, status, from, from, to, to,
	).Scan(&count); err != nil {
		return nil, 0, err
	}

	return readModels, count, nil
}

func scanInvoiceReadModels(rows *sql.Rows) ([]domain.InvoiceReadModel, error) {
	readModels := make([]domain.InvoiceReadModel, 0)
	for rows.Next() {
		var m domain.InvoiceReadModel
		var tripID sql.NullString
		var tenantID string
		var customerCompany, bookingNumber, tripNumber sql.NullString

		if err := rows.Scan(
			&m.ID, &m.InvoiceNumber, &m.BookingID, &m.CustomerID, &tripID,
			&m.Subtotal, &m.Tax, &m.Discount, &m.Total, &m.PaymentStatus, &tenantID, &m.CreatedAt, &m.UpdatedAt,
			&m.CustomerName, &customerCompany, &bookingNumber, &tripNumber,
		); err != nil {
			return nil, err
		}

		if tripID.Valid {
			m.TripID = &tripID.String
		}
		m.CustomerCompany = customerCompany.String
		m.BookingNumber = bookingNumber.String
		m.TripNumber = tripNumber.String

		readModels = append(readModels, m)
	}
	return readModels, rows.Err()
}
