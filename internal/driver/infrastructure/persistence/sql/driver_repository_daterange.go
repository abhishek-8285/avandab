package sql

import (
	"context"
	"database/sql"

	db "transport-app/db/generated/sqlite"
	"transport-app/internal/driver/domain"
	"transport-app/internal/driver/infrastructure/persistence/sql/converters"
	"transport-app/internal/shared"
)

// Date-range search variant (optional interface asserted by ListDriversUseCase
// when from/to filters are present). Keeps the core DriverRepository interface
// and its mocks untouched.

const driverDateClause = `
  AND (? = '' OR substr(CAST(created_at AS TEXT), 1, 10) >= substr(CAST(? AS TEXT), 1, 10))
  AND (? = '' OR substr(CAST(created_at AS TEXT), 1, 10) <= substr(CAST(? AS TEXT), 1, 10))`

func (r *driverRepository) SearchReadModelsDateRange(ctx context.Context, tenantID shared.TenantID, query string, status string, from string, to string, limit int, offset int) ([]domain.DriverReadModel, int64, error) {
	qPattern := "%" + query + "%"

	querySQL := `
SELECT id, driver_id, first_name, last_name, phone, email, address,
    license_number, license_expiry, experience_years, status, emergency_contact_name,
    emergency_contact_phone, notes, tenant_id, created_at, updated_at
FROM drivers
WHERE tenant_id = $1
  AND ($2 = '' OR first_name LIKE $3 OR last_name LIKE $4 OR phone LIKE $5 OR license_number LIKE $6)
  AND ($7 = '' OR status = $8)` + driverDateClause + `
ORDER BY created_at DESC
LIMIT ? OFFSET ?`

	rows, err := r.dbConn.QueryContext(ctx, querySQL,
		string(tenantID),
		query, qPattern, qPattern, qPattern, qPattern,
		status, status,
		from, from, to, to,
		limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	readModels, err := scanDriverReadModels(rows)
	if err != nil {
		return nil, 0, err
	}

	countSQL := `
SELECT COUNT(*)
FROM drivers
WHERE tenant_id = $1
  AND ($2 = '' OR first_name LIKE $3 OR last_name LIKE $4 OR phone LIKE $5 OR license_number LIKE $6)
  AND ($7 = '' OR status = $8)` + driverDateClause

	var count int64
	err = r.dbConn.QueryRowContext(ctx, countSQL,
		string(tenantID),
		query, qPattern, qPattern, qPattern, qPattern,
		status, status,
		from, from, to, to,
	).Scan(&count)
	if err != nil {
		return nil, 0, err
	}

	return readModels, count, nil
}

func scanDriverReadModels(rows *sql.Rows) ([]domain.DriverReadModel, error) {
	var readModels []domain.DriverReadModel
	for rows.Next() {
		var d db.Driver
		if err := rows.Scan(
			&d.ID, &d.DriverID, &d.FirstName, &d.LastName, &d.Phone, &d.Email, &d.Address,
			&d.LicenseNumber, &d.LicenseExpiry, &d.ExperienceYears, &d.Status, &d.EmergencyContactName,
			&d.EmergencyContactPhone, &d.Notes, &d.TenantID, &d.CreatedAt, &d.UpdatedAt,
		); err != nil {
			return nil, err
		}
		readModels = append(readModels, converters.ToReadModel(d))
	}
	return readModels, nil
}
