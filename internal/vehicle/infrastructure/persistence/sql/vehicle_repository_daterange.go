package sql

import (
	"context"
	"database/sql"

	db "transport-app/db/generated/sqlite"
	"transport-app/internal/shared"
	"transport-app/internal/vehicle/domain"
	"transport-app/internal/vehicle/infrastructure/persistence/sql/converters"
)

// Date-range search variant (optional interface asserted by ListVehiclesUseCase
// when from/to filters are present). Keeps the core VehicleRepository interface
// and its mocks untouched.

const vehicleDateClause = `
  AND (? = '' OR date(substr(created_at,1,10)) >= date(?))
  AND (? = '' OR date(substr(created_at,1,10)) <= date(?))`

func (r *vehicleRepository) SearchReadModelsDateRange(ctx context.Context, tenantID shared.TenantID, query string, status string, from string, to string, limit int, offset int) ([]domain.VehicleReadModel, int64, error) {
	qPattern := "%" + query + "%"

	querySQL := `
SELECT id, registration_number, vehicle_number, vehicle_type, capacity,
    fuel_type, insurance_expiry, fitness_expiry, permit_expiry, status, current_mileage,
    tenant_id, created_at, updated_at
FROM vehicles
WHERE tenant_id = ?
  AND (? = '' OR registration_number LIKE ? OR vehicle_number LIKE ? OR vehicle_type LIKE ?)
  AND (? = '' OR status = ?)` + vehicleDateClause + `
ORDER BY created_at DESC
LIMIT ? OFFSET ?`

	rows, err := r.dbConn.QueryContext(ctx, querySQL,
		string(tenantID),
		query, qPattern, qPattern, qPattern,
		status, status,
		from, from, to, to,
		limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	readModels, err := scanVehicleReadModels(rows)
	if err != nil {
		return nil, 0, err
	}

	countSQL := `
SELECT COUNT(*)
FROM vehicles
WHERE tenant_id = ?
  AND (? = '' OR registration_number LIKE ? OR vehicle_number LIKE ? OR vehicle_type LIKE ?)
  AND (? = '' OR status = ?)` + vehicleDateClause

	var count int64
	err = r.dbConn.QueryRowContext(ctx, countSQL,
		string(tenantID),
		query, qPattern, qPattern, qPattern,
		status, status,
		from, from, to, to,
	).Scan(&count)
	if err != nil {
		return nil, 0, err
	}

	return readModels, count, nil
}

func scanVehicleReadModels(rows *sql.Rows) ([]domain.VehicleReadModel, error) {
	var readModels []domain.VehicleReadModel
	for rows.Next() {
		var v db.Vehicle
		if err := rows.Scan(
			&v.ID, &v.RegistrationNumber, &v.VehicleNumber, &v.VehicleType, &v.Capacity,
			&v.FuelType, &v.InsuranceExpiry, &v.FitnessExpiry, &v.PermitExpiry, &v.Status, &v.CurrentMileage,
			&v.TenantID, &v.CreatedAt, &v.UpdatedAt,
		); err != nil {
			return nil, err
		}
		readModels = append(readModels, converters.ToReadModel(v))
	}
	return readModels, nil
}
