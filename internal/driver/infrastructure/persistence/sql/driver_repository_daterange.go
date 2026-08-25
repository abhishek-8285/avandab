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
  AND (? = '' OR date(substr(created_at,1,10)) >= date(?))
  AND (? = '' OR date(substr(created_at,1,10)) <= date(?))`

func (r *driverRepository) SearchReadModelsDateRange(ctx context.Context, tenantID shared.TenantID, query string, status string, from string, to string, limit int, offset int) ([]domain.DriverReadModel, int64, error) {
	qPattern := "%" + query + "%"

	querySQL := `
SELECT id, driver_id, first_name, last_name, phone, email, address,
    license_number, license_expiry, experience_years, status, emergency_contact_name,
    emergency_contact_phone, notes, tenant_id, created_at, updated_at
FROM drivers
WHERE tenant_id = ?
  AND (? = '' OR first_name LIKE ? OR last_name LIKE ? OR phone LIKE ? OR license_number LIKE ?)
  AND (? = '' OR status = ?)` + driverDateClause + `
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
WHERE tenant_id = ?
  AND (? = '' OR first_name LIKE ? OR last_name LIKE ? OR phone LIKE ? OR license_number LIKE ?)
  AND (? = '' OR status = ?)` + driverDateClause

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
