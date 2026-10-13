package sqlite

import (
	"context"

	db "transport-app/db/generated/sqlite"
	"transport-app/internal/domain"
)

// Status-filtered variants for the customers list page chips (optional
// interface asserted by CustomerService). Keeps the core CustomerRepository
// interface and its mocks untouched — same pattern as users_daterange.go.

const customerSearchClause = `
  AND (? = '' OR lower(customer_code) LIKE '%' || lower(?) || '%' OR lower(name) LIKE '%' || lower(?) || '%' OR lower(company) LIKE '%' || lower(?) || '%' OR lower(phone) LIKE '%' || lower(?) || '%' OR lower(email) LIKE '%' || lower(?) || '%' OR lower(contact_person) LIKE '%' || lower(?) || '%' OR lower(internal_id) LIKE '%' || lower(?) || '%')
  AND (? = '' OR status = ?)`

const customerSearchCols = `
SELECT id, customer_code, name, title, company, contact_person, phone, email, gst, address, billing_address, internal_id, photo_url, place_uuid, meta, type, status, payment_terms_days, tenant_id, state_code, notes, created_at, updated_at
FROM customers
WHERE tenant_id = ?`

// SearchCustomersFiltered mirrors SearchCustomers with an optional exact
// status match ("" = all). Column order matches SearchCustomersRow.
func (r *SQLRepository) SearchCustomersFiltered(ctx context.Context, query string, status string, limit int, offset int) ([]domain.Customer, error) {
	rows, err := r.query(ctx, customerSearchCols+customerSearchClause+`
ORDER BY created_at DESC
LIMIT ? OFFSET ?`,
		tenantIDFromCtx(ctx),
		query, query, query, query, query, query, query, query,
		status, status,
		int64(limit), int64(offset),
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	result := make([]domain.Customer, 0)
	for rows.Next() {
		var row db.SearchCustomersRow
		if err := rows.Scan(
			&row.ID, &row.CustomerCode, &row.Name, &row.Title, &row.Company,
			&row.ContactPerson, &row.Phone, &row.Email, &row.Gst, &row.Address,
			&row.BillingAddress, &row.InternalID, &row.PhotoUrl, &row.PlaceUuid,
			&row.Meta, &row.Type, &row.Status, &row.PaymentTermsDays, &row.TenantID,
			&row.StateCode, &row.Notes, &row.CreatedAt, &row.UpdatedAt,
		); err != nil {
			return nil, err
		}
		result = append(result, toDomainCustomerFromSearchRow(row))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// CountCustomersFiltered counts customers matching SearchCustomersFiltered.
func (r *SQLRepository) CountCustomersFiltered(ctx context.Context, query string, status string) (int64, error) {
	var count int64
	err := r.queryRow(ctx, `SELECT COUNT(*) FROM customers WHERE tenant_id = ?`+customerSearchClause,
		tenantIDFromCtx(ctx),
		query, query, query, query, query, query, query, query,
		status, status,
	).Scan(&count)
	return count, err
}
