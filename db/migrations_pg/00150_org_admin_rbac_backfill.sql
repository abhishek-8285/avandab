-- PG port of 00150_org_admin_rbac_backfill.sql
-- Same registry backfill on the PG chain: seed guard-referenced permission
-- rows and grant the operational set to roles 1/2/6. Idempotent
-- ON CONFLICT guards match the 00146 PG convention.
-- +goose Up
INSERT INTO permissions (name, description) VALUES
    ('ewaybill:read', 'View e-way bills'),
    ('ewaybill:create', 'Generate e-way bills'),
    ('ewaybill:update', 'Extend, attach Part-B, or cancel e-way bills'),
    ('ewaybill:write', 'E-way bill console actions (one-tap extend)'),
    ('dashboard:read', 'View console money strip and dashboard metrics'),
    ('fastag:read', 'View FASTag balance and transactions'),
    ('fastag:update', 'Reconcile FASTag transactions and deduct tolls'),
    ('trips:cancel', 'Cancel trips'),
    ('fuel:create', 'Record fuel issues'),
    ('scorecard:update', 'Resolve driver scorecard flags'),
    ('accounting:read', 'View accounting sync status and reconciliation'),
    ('accounting:sync', 'Trigger accounting sync and contact sync'),
    ('integrations:accounting', 'Export invoices and push journal entries to accounting'),
    ('integrations:gstn', 'Validate GSTIN and fetch GSTR summaries'),
    ('users:manage', 'Manage SaaS plans pricing and mail provider pool (platform admin)')
ON CONFLICT (name) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT 1, id FROM permissions WHERE name IN (
    'ewaybill:read', 'ewaybill:create', 'ewaybill:update', 'ewaybill:write',
    'dashboard:read', 'fastag:read', 'fastag:update', 'trips:cancel',
    'fuel:create', 'scorecard:update', 'accounting:read', 'accounting:sync',
    'integrations:accounting', 'integrations:gstn', 'users:manage')
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT 2, id FROM permissions WHERE name IN (
    'ewaybill:read', 'ewaybill:create', 'ewaybill:update', 'ewaybill:write',
    'fastag:read', 'fastag:update', 'trips:cancel',
    'fuel:create', 'scorecard:update', 'integrations:gstn')
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT 6, id FROM permissions WHERE name IN (
    'ewaybill:read', 'ewaybill:create', 'ewaybill:update', 'ewaybill:write',
    'dashboard:read', 'fastag:read', 'fastag:update', 'trips:cancel',
    'fuel:create', 'scorecard:update', 'accounting:read', 'accounting:sync',
    'integrations:accounting', 'integrations:gstn',
    'errors:read', 'errors:update',
    'esg:read', 'esg:write',
    'fuel:write',
    'kharcha:approve',
    'alerts:write',
    'rag:read')
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN (
    SELECT id FROM permissions WHERE name IN (
        'ewaybill:read', 'ewaybill:create', 'ewaybill:update', 'ewaybill:write',
        'dashboard:read', 'fastag:read', 'fastag:update', 'trips:cancel',
        'fuel:create', 'scorecard:update', 'accounting:read', 'accounting:sync',
        'integrations:accounting', 'integrations:gstn', 'users:manage'));
DELETE FROM role_permissions WHERE role_id = 6 AND permission_id IN (
    SELECT id FROM permissions WHERE name IN (
        'errors:read', 'errors:update', 'esg:read', 'esg:write', 'fuel:write',
        'kharcha:approve', 'alerts:write', 'rag:read', 'dashboard:read'));
DELETE FROM permissions WHERE name IN (
    'ewaybill:read', 'ewaybill:create', 'ewaybill:update', 'ewaybill:write',
    'dashboard:read', 'fastag:read', 'fastag:update', 'trips:cancel',
    'fuel:create', 'scorecard:update', 'accounting:read', 'accounting:sync',
    'integrations:accounting', 'integrations:gstn', 'users:manage');
