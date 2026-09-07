-- PG port of 00064_org_admin_role.sql | status: PORTABLE | flags: none
-- +goose Up
-- Add 'org_admin' role for tenant-scoped organization administration
INSERT INTO roles (id, name, description) VALUES
(6, 'org_admin', 'Organization Administrator with full tenant-scoped management') ON CONFLICT DO NOTHING;
-- Grant all operational, financial, asset, and user permissions to org_admin
-- Excludes platform-wide founder signals and experiments
INSERT INTO role_permissions (role_id, permission_id)
SELECT 6, id FROM permissions
WHERE name NOT IN (
    'founder:read', 'founder:update',
    'experiments:read', 'experiments:write'
) ON CONFLICT DO NOTHING;
-- +goose Down
DELETE FROM role_permissions WHERE role_id = 6;
DELETE FROM roles WHERE id = 6;
