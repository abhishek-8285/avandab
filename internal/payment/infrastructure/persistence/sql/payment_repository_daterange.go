package sql

import (
	"context"
	"database/sql"

	"transport-app/internal/payment/domain"
	"transport-app/internal/shared"
)

// Date-range search variant (optional interface asserted by ListPaymentsUseCase
// when from/to filters are present). Raw SQL keeps the sqlc query set and
// existing mocks untouched (same coexistence pattern as
// booking_repository_daterange.go). Mirrors the sqlc SearchPayments shape:
// tenant + method filter, newest first.
//
// SQLite gotcha: timestamps are stored RFC3339 ("2026-08-15T08:00:00Z") which
// date() cannot parse — hence date(substr(p.payment_date,1,10)).

const paymentDateClause = `
  AND (? = '' OR date(substr(p.payment_date,1,10)) >= date(?))
  AND (? = '' OR date(substr(p.payment_date,1,10)) <= date(?))`

const paymentDateRangeSelect = `
SELECT p.id, p.invoice_id, p.payment_date, p.amount, p.method, p.reference, p.remarks, p.tenant_id, p.created_at, p.updated_at,
       i.invoice_number
FROM payments p
JOIN invoices i ON p.invoice_id = i.id
WHERE p.tenant_id = ?
  AND (? = '' OR p.method = ?)`

const paymentDateRangeCount = `
SELECT COUNT(*)
FROM payments p
JOIN invoices i ON p.invoice_id = i.id
WHERE p.tenant_id = ?
  AND (? = '' OR p.method = ?)`

func (r *paymentRepository) SearchReadModelsDateRange(ctx context.Context, tenantID shared.TenantID, method string, from string, to string, limit int, offset int) ([]domain.PaymentReadModel, int64, error) {
	rows, err := r.dbConn.QueryContext(ctx,
		paymentDateRangeSelect+paymentDateClause+`
ORDER BY p.payment_date DESC
LIMIT ? OFFSET ?`,
		string(tenantID), method, method, from, from, to, to, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	readModels, err := scanPaymentReadModels(rows)
	if err != nil {
		return nil, 0, err
	}

	var count int64
	if err := r.exec(ctx).QueryRowContext(ctx, paymentDateRangeCount+paymentDateClause,
		string(tenantID), method, method, from, from, to, to,
	).Scan(&count); err != nil {
		return nil, 0, err
	}

	return readModels, count, nil
}

func scanPaymentReadModels(rows *sql.Rows) ([]domain.PaymentReadModel, error) {
	readModels := make([]domain.PaymentReadModel, 0)
	for rows.Next() {
		var m domain.PaymentReadModel
		var reference, remarks sql.NullString
		var tenantID string

		if err := rows.Scan(
			&m.ID, &m.InvoiceID, &m.PaymentDate, &m.Amount, &m.Method, &reference, &remarks, &tenantID, &m.CreatedAt, &m.UpdatedAt,
			&m.InvoiceNumber,
		); err != nil {
			return nil, err
		}

		if reference.Valid {
			m.Reference = &reference.String
		}
		if remarks.Valid {
			m.Remarks = &remarks.String
		}

		readModels = append(readModels, m)
	}
	return readModels, rows.Err()
}
