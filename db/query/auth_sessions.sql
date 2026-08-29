-- Sessions
-- name: CreateSession :one
INSERT INTO sessions (id, user_id, token_hash, expires_at, user_agent, ip_address)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING id, user_id, token_hash, expires_at, user_agent, ip_address, created_at;

-- name: GetSessionByToken :one
SELECT s.id, s.user_id, s.token_hash, s.expires_at, s.user_agent, s.ip_address, s.created_at,
       u.email AS user_email, u.name AS user_name, u.role_id, r.name AS role_name, u.status AS user_status
FROM sessions s
JOIN users u ON s.user_id = u.id
JOIN roles r ON u.role_id = r.id
WHERE s.token_hash = ?;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE token_hash = ?;

-- name: DeleteSessionsForUser :exec
DELETE FROM sessions WHERE user_id = ?;

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE expires_at < datetime('now');
