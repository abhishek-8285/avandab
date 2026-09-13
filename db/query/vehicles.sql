-- name: CreateVehicle :one
INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity,
    fuel_type, insurance_expiry, fitness_expiry, permit_expiry, status, current_mileage, blocked, blocked_reason, rc_expiry, odometer, puc_expiry, tenant_id,
    fleet_class, ownership, fleet_number, description, manufacturer, manuf_country, model,
    constr_year_month, acquisition_value, acquisition_currency, acquisition_date, purchase_vendor,
    valid_from, valid_to, facility_id, maint_plant, planning_plant, company_code, business_area,
    cost_center, asset_no, fleet_object_no, chassis_no, vehicle_category, engine_number,
    engine_power, engine_capacity, cylinder_count, max_speed, weight, weight_unit, load_volume,
    volume_unit, secondary_fuel, usage_indicator, standard_kmpl)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
    ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id, registration_number, vehicle_number, vehicle_type, capacity,
    fuel_type, insurance_expiry, fitness_expiry, permit_expiry, status, current_mileage, blocked, blocked_reason, rc_expiry, odometer, puc_expiry,
    tenant_id, created_at, updated_at,
    fleet_class, ownership, fleet_number, description, manufacturer, manuf_country, model,
    constr_year_month, acquisition_value, acquisition_currency, acquisition_date, purchase_vendor,
    valid_from, valid_to, facility_id, maint_plant, planning_plant, company_code, business_area,
    cost_center, asset_no, fleet_object_no, chassis_no, vehicle_category, engine_number,
    engine_power, engine_capacity, cylinder_count, max_speed, weight, weight_unit, load_volume,
    volume_unit, secondary_fuel, usage_indicator, standard_kmpl;

-- name: GetVehicleByID :one
SELECT id, registration_number, vehicle_number, vehicle_type, capacity,
    fuel_type, insurance_expiry, fitness_expiry, permit_expiry, status, current_mileage, blocked, blocked_reason, rc_expiry, odometer, puc_expiry,
    tenant_id, created_at, updated_at,
    fleet_class, ownership, fleet_number, description, manufacturer, manuf_country, model,
    constr_year_month, acquisition_value, acquisition_currency, acquisition_date, purchase_vendor,
    valid_from, valid_to, facility_id, maint_plant, planning_plant, company_code, business_area,
    cost_center, asset_no, fleet_object_no, chassis_no, vehicle_category, engine_number,
    engine_power, engine_capacity, cylinder_count, max_speed, weight, weight_unit, load_volume,
    volume_unit, secondary_fuel, usage_indicator, standard_kmpl
FROM vehicles WHERE id = ? AND tenant_id = ?;

-- name: GetVehicleByRegistration :one
SELECT id, registration_number, vehicle_number, vehicle_type, capacity,
    fuel_type, insurance_expiry, fitness_expiry, permit_expiry, status, current_mileage, blocked, blocked_reason, rc_expiry, odometer, puc_expiry,
    tenant_id, created_at, updated_at,
    fleet_class, ownership, fleet_number, description, manufacturer, manuf_country, model,
    constr_year_month, acquisition_value, acquisition_currency, acquisition_date, purchase_vendor,
    valid_from, valid_to, facility_id, maint_plant, planning_plant, company_code, business_area,
    cost_center, asset_no, fleet_object_no, chassis_no, vehicle_category, engine_number,
    engine_power, engine_capacity, cylinder_count, max_speed, weight, weight_unit, load_volume,
    volume_unit, secondary_fuel, usage_indicator, standard_kmpl
FROM vehicles WHERE registration_number = ? AND tenant_id = ?;

-- name: UpdateVehicle :one
UPDATE vehicles
SET registration_number = ?, vehicle_number = ?, vehicle_type = ?, capacity = ?,
    fuel_type = ?, insurance_expiry = ?, fitness_expiry = ?, permit_expiry = ?,
    status = ?, current_mileage = ?, blocked = ?, blocked_reason = ?, rc_expiry = ?, odometer = ?, puc_expiry = ?,
    fleet_class = ?, ownership = ?, fleet_number = ?, description = ?, manufacturer = ?,
    manuf_country = ?, model = ?, constr_year_month = ?, acquisition_value = ?,
    acquisition_currency = ?, acquisition_date = ?, purchase_vendor = ?, valid_from = ?,
    valid_to = ?, facility_id = ?, maint_plant = ?, planning_plant = ?, company_code = ?,
    business_area = ?, cost_center = ?, asset_no = ?, fleet_object_no = ?, chassis_no = ?,
    vehicle_category = ?, engine_number = ?, engine_power = ?, engine_capacity = ?,
    cylinder_count = ?, max_speed = ?, weight = ?, weight_unit = ?, load_volume = ?,
    volume_unit = ?, secondary_fuel = ?, usage_indicator = ?,
    standard_kmpl = ?,
    updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND tenant_id = ?
RETURNING id, registration_number, vehicle_number, vehicle_type, capacity,
    fuel_type, insurance_expiry, fitness_expiry, permit_expiry, status, current_mileage, blocked, blocked_reason, rc_expiry, odometer, puc_expiry,
    tenant_id, created_at, updated_at,
    fleet_class, ownership, fleet_number, description, manufacturer, manuf_country, model,
    constr_year_month, acquisition_value, acquisition_currency, acquisition_date, purchase_vendor,
    valid_from, valid_to, facility_id, maint_plant, planning_plant, company_code, business_area,
    cost_center, asset_no, fleet_object_no, chassis_no, vehicle_category, engine_number,
    engine_power, engine_capacity, cylinder_count, max_speed, weight, weight_unit, load_volume,
    volume_unit, secondary_fuel, usage_indicator, standard_kmpl;

-- name: DeleteVehicle :exec
DELETE FROM vehicles WHERE id = ? AND tenant_id = ?;

-- name: SearchVehicles :many
SELECT id, registration_number, vehicle_number, vehicle_type, capacity,
    fuel_type, insurance_expiry, fitness_expiry, permit_expiry, status, current_mileage, blocked, blocked_reason, rc_expiry, odometer, puc_expiry,
    tenant_id, created_at, updated_at,
    fleet_class, ownership, fleet_number, description, manufacturer, manuf_country, model,
    constr_year_month, acquisition_value, acquisition_currency, acquisition_date, purchase_vendor,
    valid_from, valid_to, facility_id, maint_plant, planning_plant, company_code, business_area,
    cost_center, asset_no, fleet_object_no, chassis_no, vehicle_category, engine_number,
    engine_power, engine_capacity, cylinder_count, max_speed, weight, weight_unit, load_volume,
    volume_unit, secondary_fuel, usage_indicator, standard_kmpl
FROM vehicles
WHERE tenant_id = sqlc.arg(tenant_id)
  AND (lower(registration_number) LIKE '%' || lower(sqlc.arg(search)) || '%' OR lower(vehicle_number) LIKE '%' || lower(sqlc.arg(search)) || '%' OR lower(vehicle_type) LIKE '%' || lower(sqlc.arg(search)) || '%'
    OR lower(manufacturer) LIKE '%' || lower(sqlc.arg(search)) || '%' OR lower(model) LIKE '%' || lower(sqlc.arg(search)) || '%' OR lower(facility_id) LIKE '%' || lower(sqlc.arg(search)) || '%')
  AND (sqlc.arg(status_all) = '' OR status = sqlc.arg(status))
  AND (sqlc.arg(fleet_class_all) = '' OR fleet_class = sqlc.arg(fleet_class))
  AND (sqlc.arg(ownership_all) = '' OR ownership = sqlc.arg(ownership))
ORDER BY created_at DESC
LIMIT sqlc.arg(limit) OFFSET sqlc.arg(offset);

-- name: CountVehicles :one
SELECT COUNT(*) AS count
FROM vehicles
WHERE tenant_id = sqlc.arg(tenant_id)
  AND (lower(registration_number) LIKE '%' || lower(sqlc.arg(search)) || '%' OR lower(vehicle_number) LIKE '%' || lower(sqlc.arg(search)) || '%' OR lower(vehicle_type) LIKE '%' || lower(sqlc.arg(search)) || '%'
    OR lower(manufacturer) LIKE '%' || lower(sqlc.arg(search)) || '%' OR lower(model) LIKE '%' || lower(sqlc.arg(search)) || '%' OR lower(facility_id) LIKE '%' || lower(sqlc.arg(search)) || '%')
  AND (sqlc.arg(status_all) = '' OR status = sqlc.arg(status))
  AND (sqlc.arg(fleet_class_all) = '' OR fleet_class = sqlc.arg(fleet_class))
  AND (sqlc.arg(ownership_all) = '' OR ownership = sqlc.arg(ownership));

-- name: CountAvailableVehicles :one
SELECT COUNT(*) AS count
FROM vehicles
WHERE status = 'available' AND tenant_id = ?;

-- name: GetAvailableVehicles :many
SELECT id, registration_number, vehicle_number, vehicle_type, capacity,
    fuel_type, insurance_expiry, fitness_expiry, permit_expiry, status, current_mileage, blocked, blocked_reason, rc_expiry, odometer, puc_expiry,
    tenant_id, created_at, updated_at,
    fleet_class, ownership, fleet_number, description, manufacturer, manuf_country, model,
    constr_year_month, acquisition_value, acquisition_currency, acquisition_date, purchase_vendor,
    valid_from, valid_to, facility_id, maint_plant, planning_plant, company_code, business_area,
    cost_center, asset_no, fleet_object_no, chassis_no, vehicle_category, engine_number,
    engine_power, engine_capacity, cylinder_count, max_speed, weight, weight_unit, load_volume,
    volume_unit, secondary_fuel, usage_indicator, standard_kmpl
FROM vehicles
WHERE status = 'available' AND tenant_id = ?
ORDER BY created_at ASC;

-- name: GetIdleVehicles :many
SELECT id, registration_number, vehicle_number, vehicle_type, capacity,
    fuel_type, insurance_expiry, fitness_expiry, permit_expiry, status, current_mileage, blocked, blocked_reason, rc_expiry, odometer, puc_expiry,
    tenant_id, created_at, updated_at,
    fleet_class, ownership, fleet_number, description, manufacturer, manuf_country, model,
    constr_year_month, acquisition_value, acquisition_currency, acquisition_date, purchase_vendor,
    valid_from, valid_to, facility_id, maint_plant, planning_plant, company_code, business_area,
    cost_center, asset_no, fleet_object_no, chassis_no, vehicle_category, engine_number,
    engine_power, engine_capacity, cylinder_count, max_speed, weight, weight_unit, load_volume,
    volume_unit, secondary_fuel, usage_indicator, standard_kmpl
FROM vehicles
-- Stale bound is a Go-side param (impl passes UTC now-2h, matching the old
-- datetime('now', '-2 hours')) so the query stays portable.
WHERE status = 'available' AND tenant_id = ? AND updated_at < sqlc.arg(stale_before)
ORDER BY created_at ASC
LIMIT 10;
