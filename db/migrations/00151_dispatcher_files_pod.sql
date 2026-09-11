-- +goose Up
-- Dispatcher (role 2) runs trips daily: viewing POD photos and uploading
-- them is core dispatcher work, but files:read/create were never granted
-- beyond admin/org_admin/viewer. Without this, the trip ePOD section 403s
-- file reads and rejects uploads for dispatchers.
INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT 2, id FROM permissions WHERE name IN ('files:read', 'files:create');

-- +goose Down
DELETE FROM role_permissions WHERE role_id = 2 AND permission_id IN (
    SELECT id FROM permissions WHERE name IN ('files:read', 'files:create'));
