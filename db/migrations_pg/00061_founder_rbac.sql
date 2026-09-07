-- PG port of 00061_founder_rbac.sql | status: PORTABLE | flags: none
-- +goose Up
-- Spec 16 §6, §7: RBAC permissions for the founder visibility layer
-- (founder signals, audit trail, dashboard). Admin-only by default.
INSERT INTO permissions (name, description) VALUES
('founder:read', 'View founder signals, audit trail, and dashboard'),
('founder:update', 'Acknowledge founder signals') ON CONFLICT DO NOTHING;
-- Admin role (role id 1 per 00012 pattern) gets full access.
INSERT INTO role_permissions (role_id, permission_id)
SELECT 1, id FROM permissions WHERE name IN ('founder:read','founder:update') ON CONFLICT DO NOTHING;
-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN
(SELECT id FROM permissions WHERE name IN ('founder:read','founder:update'));
DELETE FROM permissions WHERE name IN ('founder:read','founder:update');
