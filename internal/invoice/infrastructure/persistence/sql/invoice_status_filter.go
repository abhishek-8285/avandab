package sql

import (
	"context"

	"transport-app/internal/invoice/domain"
	"transport-app/internal/shared"
)

// StatusFilterOpen is the invoice list pseudo-status for every unpaid
// invoice: pending + partially_paid. The dashboard Pending Payments card
// counts exactly this set (GetPendingInvoices), so its drill-down links
// here (e.g. /invoices?status=open) instead of to pending-only.
//
// Raw SQL keeps the sqlc query set and existing mocks untouched (same
// coexistence pattern as the daterange search variants). Column shape
// mirrors SearchInvoices so scanInvoiceReadModels stays the single scanner.
const StatusFilterOpen = "open"

const outstandingInvoiceSelect = `
SELECT i.id, i.invoice_number, i.booking_id, i.customer_id, i.trip_id,
    i.subtotal, i.tax, i.discount, i.total, i.payment_status, i.tenant_id, i.created_at, i.updated_at,
    c.name AS customer_name, c.company AS customer_company, b.booking_number, t.trip_number
FROM invoices i
JOIN customers c ON i.customer_id = c.id
LEFT JOIN bookings b ON i.booking_id = b.id
LEFT JOIN trips t ON i.trip_id = t.id
WHERE i.tenant_id = ?
  AND (i.invoice_number LIKE '%' || ? || '%' OR c.name LIKE '%' || ? || '%')
  AND i.payment_status IN ('pending', 'partially_paid')`

const outstandingInvoiceCount = `
SELECT COUNT(*)
FROM invoices i
JOIN customers c ON i.customer_id = c.id
WHERE i.tenant_id = ?
  AND (i.invoice_number LIKE '%' || ? || '%' OR c.name LIKE '%' || ? || '%')
  AND i.payment_status IN ('pending', 'partially_paid')`

// searchOutstandingInvoices lists unpaid invoices, optionally bounded by an
// invoice-date window (from/to empty = unbounded).
func (r *invoiceRepository) searchOutstandingInvoices(ctx context.Context, tenantID shared.TenantID, query, from, to string, limit, offset int) ([]domain.InvoiceReadModel, int64, error) {
	selectSQL := outstandingInvoiceSelect
	countSQL := outstandingInvoiceCount
	args := []any{string(tenantID), query, query}
	if from != "" || to != "" {
		selectSQL += invoiceDateClause
		countSQL += invoiceDateClause
		args = append(args, from, from, to, to)
	}

	rows, err := r.exec(ctx).QueryContext(ctx, selectSQL+`
ORDER BY i.created_at DESC
LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	readModels, err := scanInvoiceReadModels(rows)
	if err != nil {
		return nil, 0, err
	}

	var count int64
	if err := r.exec(ctx).QueryRowContext(ctx, countSQL, args...).Scan(&count); err != nil {
		return nil, 0, err
	}

	return readModels, count, nil
}
