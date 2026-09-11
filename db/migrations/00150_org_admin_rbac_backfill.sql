-- +goose Up
-- RBAC registry backfill: every permission string referenced by a route
-- guard or sidebar `can` check MUST exist as a row, and org_admin (role 6)
-- MUST hold what the UI promises an org owner.
--
-- Disease: 00064 granted role 6 "every permission" as of its date, but every
-- later migration adding permissions granted only roles 1/2 — and several
-- guards referenced rows that never existed anywhere (startup Go seeders
-- covered two of them, invisibly). Net effect found by live crawl + registry
-- scan: /ewaybill and /fastag 403'd for EVERY role; org_admin 403'd on
-- error reports, ESG, fuel cards, money approvals, alert ack, trip cancel,
-- scorecard resolve, and the console money-strip.
--
-- Deliberately NOT granted to role 6: founder:*, experiments:*
-- (platform-only per 00064), features:update (platform-controlled toggles;
-- see settings.go), tenants:manage (platform), users:manage (SaaS pricing +
-- mail infra admin APIs), customer_portal:* (customer role 7 surface),
-- driver:*-self (driver self-scope), vehicles:command (no guard references
-- it), rag:write (corpus management stays platform-admin).

-- Single source of truth for guard-referenced rows. (dashboard:read and
-- ewaybill:write were previously startup-seeder-only in cmd/server/main.go;
-- the seeders stay as idempotent backstops.)
INSERT OR IGNORE INTO permissions (name, description) VALUES
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
    ('users:manage', 'Manage SaaS plans pricing and mail provider pool (platform admin)');

-- Admin (1) gets everything new except nothing (full row).
INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT 1, id FROM permissions WHERE name IN (
    'ewaybill:read', 'ewaybill:create', 'ewaybill:update', 'ewaybill:write',
    'dashboard:read', 'fastag:read', 'fastag:update', 'trips:cancel',
    'fuel:create', 'scorecard:update', 'accounting:read', 'accounting:sync',
    'integrations:accounting', 'integrations:gstn', 'users:manage');

-- Dispatcher (2) runs daily ops: EWB, FASTag, cancel, fuel issues,
-- scorecard coaching, GSTIN checks (mirrors integrations:ewaybill + errors:read).
INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT 2, id FROM permissions WHERE name IN (
    'ewaybill:read', 'ewaybill:create', 'ewaybill:update', 'ewaybill:write',
    'fastag:read', 'fastag:update', 'trips:cancel',
    'fuel:create', 'scorecard:update', 'integrations:gstn');

-- Org Admin (6): everything operational above, plus the post-00064 backlog
-- the owner was missing (errors, ESG, fuel cards, money approvals, alerts,
-- RAG answers, money-strip).
INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
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
    'rag:read');

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN (
    SELECT id FROM permissions WHERE name IN (
        'ewaybill:read', 'ewaybill:create', 'ewaybill:update', 'ewaybill:write',
        'dashboard:read', 'fastag:read', 'fastag:update', 'trips:cancel',
        'fuel:create', 'scorecard:update', 'accounting:read', 'accounting:sync',
        'integrations:accounting', 'integrations:gstn', 'users:manage',
        'errors:read', 'errors:update', 'esg:read', 'esg:write', 'fuel:write',
        'kharcha:approve', 'alerts:write', 'rag:read'));
DELETE FROM role_permissions WHERE role_id = 6 AND permission_id IN (
    SELECT id FROM permissions WHERE name IN (
        'errors:read', 'errors:update', 'esg:read', 'esg:write', 'fuel:write',
        'kharcha:approve', 'alerts:write', 'rag:read', 'dashboard:read'));
DELETE FROM permissions WHERE name IN (
    'ewaybill:read', 'ewaybill:create', 'ewaybill:update', 'ewaybill:write',
    'dashboard:read', 'fastag:read', 'fastag:update', 'trips:cancel',
    'fuel:create', 'scorecard:update', 'accounting:read', 'accounting:sync',
    'integrations:accounting', 'integrations:gstn', 'users:manage');
