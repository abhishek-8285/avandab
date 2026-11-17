-- +goose Up
-- 00189: the kharcha dashboard, pending queue, ledger, and manager API are
-- manager surfaces. The driver role (5) holds trips:read (00027), which
-- previously opened every pending expense claim in the tenant — including
-- other drivers' amounts, notes, and receipt URLs. Split read access into its
-- own permission so drivers keep trips:read without seeing tenant-wide money.
--
-- Approval stays stricter: kharcha:approve is admin (1) + org_admin (6) only
-- (00186), so dispatcher and driver remain denied by the RBAC ratchet.
INSERT OR IGNORE INTO permissions (name, description)
VALUES ('kharcha:read', 'View driver expense ledger and pending claims');

INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT r.role_id, p.id
FROM (SELECT 1 AS role_id
      UNION ALL SELECT 2
      UNION ALL SELECT 3
      UNION ALL SELECT 4
      UNION ALL SELECT 6) r
JOIN permissions p ON p.name = 'kharcha:read';

-- +goose Down
DELETE FROM role_permissions
WHERE permission_id IN (SELECT id FROM permissions WHERE name = 'kharcha:read');
DELETE FROM permissions WHERE name = 'kharcha:read';
