-- Roles
-- name: GetRoleByName :one
SELECT id, name, description, created_at, updated_at
FROM roles WHERE name = ?;

-- name: GetRoleByID :one
SELECT id, name, description, created_at, updated_at
FROM roles WHERE id = ?;

-- name: ListRoles :many
SELECT id, name, description, created_at, updated_at
FROM roles ORDER BY id ASC;

-- Users
-- name: GetUserByID :one
SELECT id, email, password_hash, tenant_id, name, phone, role_id, status, last_login_at, theme_preference, created_at, updated_at
FROM users WHERE id = ?;

-- name: GetUserByEmail :one
SELECT id, email, password_hash, tenant_id, name, phone, role_id, status, last_login_at, theme_preference, created_at, updated_at
FROM users WHERE email = ?;

-- name: CreateUser :one
INSERT INTO users (id, email, password_hash, name, phone, role_id, status, tenant_id, theme_preference)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id, email, password_hash, tenant_id, name, phone, role_id, status, last_login_at, theme_preference, created_at, updated_at;

-- name: UpdateUser :one
UPDATE users
SET email = ?, name = ?, phone = ?, role_id = ?, status = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?
RETURNING id, email, password_hash, tenant_id, name, phone, role_id, status, last_login_at, theme_preference, created_at, updated_at;

-- name: UpdateUserThemePreference :one
UPDATE users
SET theme_preference = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?
RETURNING id, email, password_hash, tenant_id, name, phone, role_id, status, last_login_at, theme_preference, created_at, updated_at;

-- name: UpdateUserPassword :one
UPDATE users
SET password_hash = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?
RETURNING id, email, password_hash, tenant_id, name, phone, role_id, status, last_login_at, theme_preference, created_at, updated_at;

-- name: UpdateUserLastLogin :one
UPDATE users
SET last_login_at = CURRENT_TIMESTAMP
WHERE id = ?
RETURNING id, email, password_hash, tenant_id, name, phone, role_id, status, last_login_at, theme_preference, created_at, updated_at;

-- name: DeleteUser :exec
DELETE FROM users WHERE id = ? AND tenant_id = ?;

-- name: SearchUsers :many
SELECT u.id, u.email, u.tenant_id, u.name, u.phone, u.role_id, u.status, u.last_login_at, u.theme_preference, u.created_at, u.updated_at,
       r.name AS role_name
FROM users u
JOIN roles r ON u.role_id = r.id
WHERE u.tenant_id = sqlc.arg(tenant_id)
  AND (lower(u.name) LIKE '%' || lower(sqlc.arg(search)) || '%' OR lower(u.email) LIKE '%' || lower(sqlc.arg(search)) || '%')
  AND (sqlc.arg(status_all) = '' OR u.status = sqlc.arg(status))
ORDER BY u.created_at DESC
LIMIT sqlc.arg(limit) OFFSET sqlc.arg(offset);

-- name: CountUsers :one
SELECT COUNT(*) AS count
FROM users
WHERE tenant_id = sqlc.arg(tenant_id)
  AND (lower(name) LIKE '%' || lower(sqlc.arg(search)) || '%' OR lower(email) LIKE '%' || lower(sqlc.arg(search)) || '%')
  AND (sqlc.arg(status_all) = '' OR status = sqlc.arg(status));
