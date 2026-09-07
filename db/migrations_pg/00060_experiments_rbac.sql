-- PG port of 00060_experiments_rbac.sql | status: PORTABLE | flags: none
-- +goose Up
-- Spec 16 §5: RBAC permissions for the A/B experiments service + feature flag API.
INSERT INTO permissions (name, description) VALUES
('experiments:read', 'View experiments, assignments and feature flag evaluation'),
('experiments:write', 'Create and manage experiments (lifecycle, metrics)') ON CONFLICT DO NOTHING;
-- Assign to admin role (role id 1 per 00012 pattern).
INSERT INTO role_permissions (role_id, permission_id)
SELECT 1, id FROM permissions WHERE name IN ('experiments:read','experiments:write') ON CONFLICT DO NOTHING;
-- Assign read to dispatcher (role id 2) and accountant (role id 3) where present.
INSERT INTO role_permissions (role_id, permission_id)
SELECT 2, id FROM permissions WHERE name IN ('experiments:read') ON CONFLICT DO NOTHING;
INSERT INTO role_permissions (role_id, permission_id)
SELECT 3, id FROM permissions WHERE name IN ('experiments:read') ON CONFLICT DO NOTHING;
-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN
(SELECT id FROM permissions WHERE name IN ('experiments:read','experiments:write'));
DELETE FROM permissions WHERE name IN ('experiments:read','experiments:write');
