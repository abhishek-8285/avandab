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
  AND (? = '' OR substr(CAST(created_at AS TEXT), 1, 10) >= substr(CAST(? AS TEXT), 1, 10))
  AND (? = '' OR substr(CAST(created_at AS TEXT), 1, 10) <= substr(CAST(? AS TEXT), 1, 10))`

const vehicleFullColumns = `
SELECT id, registration_number, vehicle_number, vehicle_type, capacity,
    fuel_type, insurance_expiry, fitness_expiry, permit_expiry, status, current_mileage,
    tenant_id, created_at, updated_at,
    fleet_class, ownership, fleet_number, description, manufacturer, manuf_country, model,
    constr_year_month, acquisition_value, acquisition_currency, acquisition_date, purchase_vendor,
    valid_from, valid_to, facility_id, maint_plant, planning_plant, company_code, business_area,
    cost_center, asset_no, fleet_object_no, chassis_no, vehicle_category, engine_number,
    engine_power, engine_capacity, cylinder_count, max_speed, weight, weight_unit, load_volume,
    volume_unit, secondary_fuel, usage_indicator`

func (r *vehicleRepository) SearchReadModelsDateRange(ctx context.Context, tenantID shared.TenantID, query string, status string, from string, to string, limit int, offset int) ([]domain.VehicleReadModel, int64, error) {
	return r.searchDateRange(ctx, tenantID, query, status, "", "", from, to, limit, offset)
}

// SearchReadModelsFiltered adds the SOP fleet_class / ownership filters to the
// date-range search. Asserted optionally by ListVehiclesUseCase so existing
// repository implementations/mocks keep compiling unchanged.
func (r *vehicleRepository) SearchReadModelsFiltered(ctx context.Context, tenantID shared.TenantID, query string, status string, fleetClass string, ownership string, from string, to string, limit int, offset int) ([]domain.VehicleReadModel, int64, error) {
	return r.searchDateRange(ctx, tenantID, query, status, fleetClass, ownership, from, to, limit, offset)
}

func (r *vehicleRepository) searchDateRange(ctx context.Context, tenantID shared.TenantID, query string, status string, fleetClass string, ownership string, from string, to string, limit int, offset int) ([]domain.VehicleReadModel, int64, error) {
	qPattern := "%" + query + "%"

	querySQL := vehicleFullColumns + `
FROM vehicles
WHERE tenant_id = ?
  AND (? = '' OR registration_number LIKE ? OR vehicle_number LIKE ? OR vehicle_type LIKE ?
    OR manufacturer LIKE ? OR model LIKE ? OR facility_id LIKE ?)
  AND (? = '' OR status = ?)
  AND (? = '' OR fleet_class = ?)
  AND (? = '' OR ownership = ?)` + vehicleDateClause + `
ORDER BY created_at DESC
LIMIT ? OFFSET ?`

	rows, err := r.dbConn.QueryContext(ctx, querySQL,
		string(tenantID),
		query, qPattern, qPattern, qPattern, qPattern, qPattern, qPattern,
		status, status,
		fleetClass, fleetClass,
		ownership, ownership,
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
WHERE tenant_id = $1
  AND ($2 = '' OR registration_number LIKE $3 OR vehicle_number LIKE $4 OR vehicle_type LIKE $5
    OR manufacturer LIKE $6 OR model LIKE $7 OR facility_id LIKE $8)
  AND ($9 = '' OR status = $10)
  AND ($11 = '' OR fleet_class = $12)
  AND ($13 = '' OR ownership = $14)` + vehicleDateClause

	var count int64
	err = r.dbConn.QueryRowContext(ctx, countSQL,
		string(tenantID),
		query, qPattern, qPattern, qPattern, qPattern, qPattern, qPattern,
		status, status,
		fleetClass, fleetClass,
		ownership, ownership,
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
			&v.FleetClass, &v.Ownership, &v.FleetNumber, &v.Description, &v.Manufacturer,
			&v.ManufCountry, &v.Model, &v.ConstrYearMonth, &v.AcquisitionValue, &v.AcquisitionCurrency,
			&v.AcquisitionDate, &v.PurchaseVendor, &v.ValidFrom, &v.ValidTo, &v.FacilityID,
			&v.MaintPlant, &v.PlanningPlant, &v.CompanyCode, &v.BusinessArea, &v.CostCenter,
			&v.AssetNo, &v.FleetObjectNo, &v.ChassisNo, &v.VehicleCategory, &v.EngineNumber,
			&v.EnginePower, &v.EngineCapacity, &v.CylinderCount, &v.MaxSpeed, &v.Weight,
			&v.WeightUnit, &v.LoadVolume, &v.VolumeUnit, &v.SecondaryFuel, &v.UsageIndicator,
		); err != nil {
			return nil, err
		}
		readModels = append(readModels, converters.ToReadModel(v))
	}
	return readModels, nil
}
