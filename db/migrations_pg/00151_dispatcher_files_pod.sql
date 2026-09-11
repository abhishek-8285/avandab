-- PG port of 00151_dispatcher_files_pod.sql
-- Dispatcher (role 2) gets files:read/create for trip ePOD work.
-- +goose Up
INSERT INTO role_permissions (role_id, permission_id)
SELECT 2, id FROM permissions WHERE name IN ('files:read', 'files:create')
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions WHERE role_id = 2 AND permission_id IN (
    SELECT id FROM permissions WHERE name IN ('files:read', 'files:create'));
