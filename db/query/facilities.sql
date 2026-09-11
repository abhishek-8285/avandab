-- name: CreateFacility :one
INSERT INTO facilities (
    id, tenant_id, facility_code, name, facility_type,
    plant, circle, profit_center, cost_center,
    address, city, state, pincode,
    latitude, longitude, valid_from, valid_to, is_active
) VALUES (
    ?, ?, ?, ?, ?,
    ?, ?, ?, ?,
    ?, ?, ?, ?,
    ?, ?, ?, ?, ?
)
RETURNING *;

-- name: GetFacilityByID :one
SELECT * FROM facilities
WHERE id = ? AND tenant_id = ?;

-- name: GetFacilityByCode :one
SELECT * FROM facilities
WHERE facility_code = ? AND tenant_id = ?;

-- name: UpdateFacility :one
UPDATE facilities
SET name = ?,
    facility_type = ?,
    plant = ?,
    circle = ?,
    profit_center = ?,
    cost_center = ?,
    address = ?,
    city = ?,
    state = ?,
    pincode = ?,
    latitude = ?,
    longitude = ?,
    valid_from = ?,
    valid_to = ?,
    is_active = ?,
    updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND tenant_id = ?
RETURNING *;

-- name: SearchFacilities :many
SELECT * FROM facilities
WHERE tenant_id = sqlc.arg(tenant_id)
  AND (
    sqlc.arg(search) = ''
    OR facility_code LIKE '%' || sqlc.arg(search) || '%'
    OR lower(name) LIKE '%' || lower(sqlc.arg(search)) || '%'
    OR lower(city) LIKE '%' || lower(sqlc.arg(search)) || '%'
  )
  AND (
    sqlc.arg(facility_type) = ''
    OR facility_type = sqlc.arg(facility_type)
  )
  AND (
    sqlc.arg(active_all) != ''
    OR is_active = sqlc.arg(is_active)
  )
ORDER BY facility_code ASC
LIMIT sqlc.arg(limit) OFFSET sqlc.arg(offset);

-- name: CountFacilities :one
SELECT COUNT(*) AS count FROM facilities
WHERE tenant_id = sqlc.arg(tenant_id)
  AND (
    sqlc.arg(search) = ''
    OR facility_code LIKE '%' || sqlc.arg(search) || '%'
    OR lower(name) LIKE '%' || lower(sqlc.arg(search)) || '%'
    OR lower(city) LIKE '%' || lower(sqlc.arg(search)) || '%'
  )
  AND (
    sqlc.arg(facility_type) = ''
    OR facility_type = sqlc.arg(facility_type)
  )
  AND (
    sqlc.arg(active_all) != ''
    OR is_active = sqlc.arg(is_active)
  );

-- name: DeleteFacility :exec
DELETE FROM facilities
WHERE id = ? AND tenant_id = ?;
