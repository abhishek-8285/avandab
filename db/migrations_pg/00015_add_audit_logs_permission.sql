-- PG port of 00015_add_audit_logs_permission.sql | status: PORTABLE | flags: none
-- +goose Up
-- Add missing audit_logs and files permissions
INSERT INTO permissions (name, description) VALUES
    ('audit_logs:read', 'Read audit logs'),
    ('files:create', 'Upload files'),
    ('files:read', 'Read files'),
    ('files:delete', 'Delete files') ON CONFLICT DO NOTHING;
-- Assign to admin (id: 1) - gets all permissions
INSERT INTO role_permissions (role_id, permission_id)
SELECT 1, id FROM permissions WHERE name = 'audit_logs:read' ON CONFLICT DO NOTHING;
-- Assign to dispatcher (id: 2) - can view audit logs
INSERT INTO role_permissions (role_id, permission_id)
SELECT 2, id FROM permissions WHERE name = 'audit_logs:read' ON CONFLICT DO NOTHING;
-- +goose Down
DELETE FROM role_permissions
WHERE permission_id IN (
    SELECT id FROM permissions 
    WHERE name IN ('audit_logs:read', 'files:create', 'files:read', 'files:delete')
);
DELETE FROM permissions 
WHERE name IN ('audit_logs:read', 'files:create', 'files:read', 'files:delete');
