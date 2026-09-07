-- name: CreateDriver :one
INSERT INTO drivers (id, driver_id, first_name, last_name, phone, email, address,
    license_number, license_expiry, experience_years, status, emergency_contact_name,
    emergency_contact_phone, notes, tenant_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id, driver_id, first_name, last_name, phone, email, address,
    license_number, license_expiry, experience_years, status, emergency_contact_name,
    emergency_contact_phone, notes, tenant_id, created_at, updated_at;

-- name: GetDriverByID :one
SELECT id, driver_id, first_name, last_name, phone, email, address,
    license_number, license_expiry, experience_years, status, emergency_contact_name,
    emergency_contact_phone, notes, tenant_id, created_at, updated_at
FROM drivers WHERE id = ? AND tenant_id = ?;

-- name: GetDriverByDriverID :one
SELECT id, driver_id, first_name, last_name, phone, email, address,
    license_number, license_expiry, experience_years, status, emergency_contact_name,
    emergency_contact_phone, notes, tenant_id, created_at, updated_at
FROM drivers WHERE driver_id = ? AND tenant_id = ?;

-- name: UpdateDriver :one
UPDATE drivers
SET driver_id = ?, first_name = ?, last_name = ?, phone = ?, email = ?, address = ?,
    license_number = ?, license_expiry = ?, experience_years = ?, status = ?,
    emergency_contact_name = ?, emergency_contact_phone = ?, notes = ?,
    updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND tenant_id = ?
RETURNING id, driver_id, first_name, last_name, phone, email, address,
    license_number, license_expiry, experience_years, status, emergency_contact_name,
    emergency_contact_phone, notes, tenant_id, created_at, updated_at;

-- name: DeleteDriver :exec
DELETE FROM drivers WHERE id = ? AND tenant_id = ?;

-- name: SearchDrivers :many
SELECT id, driver_id, first_name, last_name, phone, email, address,
    license_number, license_expiry, experience_years, status, emergency_contact_name,
    emergency_contact_phone, notes, tenant_id, created_at, updated_at
FROM drivers
WHERE tenant_id = sqlc.arg(tenant_id)
  AND (lower(first_name) LIKE '%' || lower(sqlc.arg(search)) || '%' OR lower(last_name) LIKE '%' || lower(sqlc.arg(search)) || '%' OR lower(phone) LIKE '%' || lower(sqlc.arg(search)) || '%' OR lower(license_number) LIKE '%' || lower(sqlc.arg(search)) || '%')
  AND (sqlc.arg(status_all) = '' OR status = sqlc.arg(status))
ORDER BY created_at DESC
LIMIT sqlc.arg(limit) OFFSET sqlc.arg(offset);

-- name: CountDrivers :one
SELECT COUNT(*) AS count
FROM drivers
WHERE tenant_id = sqlc.arg(tenant_id)
  AND (lower(first_name) LIKE '%' || lower(sqlc.arg(search)) || '%' OR lower(last_name) LIKE '%' || lower(sqlc.arg(search)) || '%' OR lower(phone) LIKE '%' || lower(sqlc.arg(search)) || '%' OR lower(license_number) LIKE '%' || lower(sqlc.arg(search)) || '%')
  AND (sqlc.arg(status_all) = '' OR status = sqlc.arg(status));

-- name: GetAvailableDrivers :many
SELECT id, driver_id, first_name, last_name, phone, email, address,
    license_number, license_expiry, experience_years, status, emergency_contact_name,
    emergency_contact_phone, notes, tenant_id, created_at, updated_at
FROM drivers
WHERE status = 'available' AND tenant_id = ?
ORDER BY created_at ASC;

-- name: GetDriverByPhone :one
SELECT id, driver_id, first_name, last_name, phone, email, address,
    license_number, license_expiry, experience_years, status, emergency_contact_name,
    emergency_contact_phone, notes, tenant_id, created_at, updated_at
FROM drivers WHERE phone = ? AND tenant_id = ?;
