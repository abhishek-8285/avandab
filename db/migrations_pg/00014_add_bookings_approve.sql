-- PG port of 00014_add_bookings_approve.sql | status: PORTABLE | flags: none
-- +goose Up
-- Add missing bookings:approve permission and assign to roles
INSERT INTO permissions (name, description)
VALUES ('bookings:approve', 'Approve bookings') ON CONFLICT DO NOTHING;
-- Assign to admin role (id: 1) - gets all permissions
INSERT INTO role_permissions (role_id, permission_id)
SELECT 1, id FROM permissions WHERE name = 'bookings:approve' ON CONFLICT DO NOTHING;
-- Assign to dispatcher role (id: 2) - gets all bookings permissions
INSERT INTO role_permissions (role_id, permission_id)
SELECT 2, id FROM permissions WHERE name = 'bookings:approve' ON CONFLICT DO NOTHING;
-- +goose Down
DELETE FROM role_permissions
WHERE permission_id = (SELECT id FROM permissions WHERE name = 'bookings:approve');
DELETE FROM permissions WHERE name = 'bookings:approve';
