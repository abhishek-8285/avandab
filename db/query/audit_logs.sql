-- name: CreateAuditLog :one
INSERT INTO audit_logs (id, user_id, action, table_name, record_id, old_values, new_values, ip_address, location, tenant_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id, user_id, action, table_name, record_id, old_values, new_values, ip_address, location, tenant_id, created_at;

-- name: GetAuditLogs :many
SELECT a.id, a.user_id, a.action, a.table_name, a.record_id, a.old_values, a.new_values, a.ip_address, a.location, a.tenant_id, a.created_at,
       u.name AS user_name
FROM audit_logs a
LEFT JOIN users u ON a.user_id = u.id
WHERE a.tenant_id = ?
ORDER BY a.created_at DESC
LIMIT ? OFFSET ?;

-- name: GetAuditLogsGlobal :many
SELECT a.id, a.user_id, a.action, a.table_name, a.record_id, a.old_values, a.new_values, a.ip_address, a.location, a.tenant_id, a.created_at,
       u.name AS user_name
FROM audit_logs a
LEFT JOIN users u ON a.user_id = u.id
ORDER BY a.created_at DESC
LIMIT ? OFFSET ?;

-- name: CountAuditLogs :one
SELECT COUNT(*) AS count
FROM audit_logs
WHERE tenant_id = ?;

-- name: CountAuditLogsGlobal :one
SELECT COUNT(*) AS count
FROM audit_logs;

-- name: CountAuditLogsSince :one
SELECT COUNT(*) AS count
FROM audit_logs
WHERE tenant_id = ? AND CAST(created_at AS TEXT) > CAST(? AS TEXT);

-- name: CountAuditLogsSinceGlobal :one
SELECT COUNT(*) AS count
FROM audit_logs
WHERE CAST(created_at AS TEXT) > CAST(? AS TEXT);

-- name: GetAuditLogsByRecord :many
SELECT a.id, a.user_id, a.action, a.table_name, a.record_id, a.old_values, a.new_values, a.ip_address, a.location, a.tenant_id, a.created_at,
       u.name AS user_name
FROM audit_logs a
LEFT JOIN users u ON a.user_id = u.id
WHERE a.tenant_id = ? AND a.table_name = ? AND a.record_id = ?
ORDER BY a.created_at DESC
LIMIT ?;

-- name: GetAuditLogsByRecordGlobal :many
SELECT a.id, a.user_id, a.action, a.table_name, a.record_id, a.old_values, a.new_values, a.ip_address, a.location, a.tenant_id, a.created_at,
       u.name AS user_name
FROM audit_logs a
LEFT JOIN users u ON a.user_id = u.id
WHERE a.table_name = ? AND a.record_id = ?
ORDER BY a.created_at DESC
LIMIT ?;
