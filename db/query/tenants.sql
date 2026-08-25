-- Tenants
-- name: GetTenantByUser :one
SELECT t.id, t.name, t.slug, t.status, t.created_at, t.updated_at
FROM tenants t
JOIN users u ON u.tenant_id = t.id
WHERE u.id = ?;

-- name: GetTenantStatusByUser :one
SELECT t.status FROM tenants t JOIN users u ON u.tenant_id = t.id WHERE u.id = ?;

-- name: InsertTenant :one
INSERT INTO tenants (id, name, slug, status) VALUES (?, ?, ?, 'active')
RETURNING id, name, slug, status, created_at, updated_at;

-- name: GetTenantByID :one
SELECT id, name, slug, status, created_at, updated_at FROM tenants WHERE id = ?;

-- name: ListTenants :many
SELECT id, name, slug, status, created_at, updated_at FROM tenants ORDER BY created_at DESC;

-- name: SetTenantStatus :exec
UPDATE tenants SET status = ?, updated_at = datetime('now') WHERE id = ?;

-- name: CountAdminsGlobal :one
SELECT COUNT(*) FROM users WHERE role_id = 1;
