-- +goose Up
-- 00186: kharcha approval is a money-authorization step, not a trip update.
-- The driver role (5) holds trips:update (00027), so approve/reject routes
-- MUST NOT be gated on it. The permission row pre-exists via 00092, but
-- partial migrations may lack it, so seed it idempotently first;
-- permissions.name is UNIQUE (00001).
--
-- Grant admin (1) and org_admin (6) only. Role 2 (dispatcher) is denied
-- kharcha:approve by the RBAC ratchet (internal/handlers/rbac_registry_test.go:
-- "dispatcher approves no money"); it lost approval only because the route
-- gate moved off trips:update, which it does hold. Driver (5) gets nothing.
INSERT OR IGNORE INTO permissions (name, description)
VALUES ('kharcha:approve', 'Approve or reject driver expense claims');

INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT r.role_id, p.id
FROM (SELECT 1 AS role_id UNION ALL SELECT 6) r
JOIN permissions p ON p.name = 'kharcha:approve';

-- +goose Down
-- Admin and org_admin grants pre-date this migration (00092, 00150), so they
-- stay. The role 2 delete is a no-op on databases migrated by this version;
-- it remains so a database that ran the earlier draft (which granted role 2)
-- rolls back to the ratchet's baseline.
DELETE FROM role_permissions
WHERE role_id = 2
  AND permission_id IN (SELECT id FROM permissions WHERE name = 'kharcha:approve');
