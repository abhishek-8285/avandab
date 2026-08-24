-- name: CreateAuditLog :one
INSERT INTO audit_logs (id, user_id, action, table_name, record_id, old_values, new_values, ip_address)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id, user_id, action, table_name, record_id, old_values, new_values, ip_address, created_at;

-- name: GetAuditLogs :many
SELECT a.id, a.user_id, a.action, a.table_name, a.record_id, a.old_values, a.new_values, a.ip_address, a.created_at,
       u.name AS user_name
FROM audit_logs a
LEFT JOIN users u ON a.user_id = u.id
ORDER BY a.created_at DESC
LIMIT ? OFFSET ?;

-- name: CountAuditLogs :one
SELECT COUNT(*) AS count
FROM audit_logs;

-- name: CountAuditLogsSince :one
SELECT COUNT(*) AS count
FROM audit_logs
WHERE created_at > :since;

-- name: GetAuditLogsByRecord :many
SELECT a.id, a.user_id, a.action, a.table_name, a.record_id, a.old_values, a.new_values, a.ip_address, a.created_at,
       u.name AS user_name
FROM audit_logs a
LEFT JOIN users u ON a.user_id = u.id
WHERE a.table_name = ? AND a.record_id = ?
ORDER BY a.created_at DESC
LIMIT ?;
