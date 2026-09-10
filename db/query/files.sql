-- name: CreateFile :one
INSERT INTO files (id, filename, original_name, path, size, mime_type, uploadable_type, uploadable_id, tenant_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id, filename, original_name, path, size, mime_type, uploadable_type, uploadable_id, created_at;

-- name: GetFileByID :one
SELECT id, filename, original_name, path, size, mime_type, uploadable_type, uploadable_id, created_at
FROM files WHERE id = ? AND tenant_id = ?;

-- name: GetFilesByUploadable :many
SELECT id, filename, original_name, path, size, mime_type, uploadable_type, uploadable_id, created_at
FROM files
WHERE uploadable_type = ? AND uploadable_id = ? AND tenant_id = ?
ORDER BY created_at ASC;

-- name: DeleteFile :exec
DELETE FROM files WHERE id = ? AND tenant_id = ?;

-- name: DeleteFilesByUploadable :exec
DELETE FROM files WHERE uploadable_type = ? AND uploadable_id = ? AND tenant_id = ?;
