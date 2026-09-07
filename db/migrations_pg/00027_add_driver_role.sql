-- PG port of 00027_add_driver_role.sql | status: PORTABLE | flags: none
-- +goose Up
-- Add 'driver' role if not exists
INSERT INTO roles (id, name, description) VALUES (5, 'driver', 'Driver access for assigned trips and status updates') ON CONFLICT DO NOTHING;
-- Driver permissions: read assigned trips, update trip status
INSERT INTO role_permissions (role_id, permission_id)
SELECT 5, id FROM permissions 
WHERE name IN ('trips:read', 'trips:update', 'routes:read', 'vehicles:read') ON CONFLICT DO NOTHING;
-- +goose Down
DELETE FROM role_permissions WHERE role_id = 5;
DELETE FROM roles WHERE id = 5;
