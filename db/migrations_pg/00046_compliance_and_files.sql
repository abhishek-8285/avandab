-- PG port of 00046_compliance_and_files.sql | status: NEEDS-REVIEW | flags: STRIPPED-PRAGMA | reviewed: YES
-- +goose Up
-- Spec 05 §5, §6: Compliance gate, PUC tracking, Document Vault files rebuild, and RBAC seeds

ALTER TABLE vehicles ADD COLUMN puc_expiry DATE;
CREATE TABLE IF NOT EXISTS compliance_exemptions (
    id           TEXT PRIMARY KEY,
    entity_type  TEXT NOT NULL CHECK (entity_type IN ('driver','vehicle')),
    entity_id    TEXT NOT NULL,
    doc_type     TEXT NOT NULL,   -- rc|fitness|insurance|puc|license
    reason       TEXT NOT NULL,
    exempt_until TIMESTAMPTZ NOT NULL,
    created_by   TEXT NOT NULL REFERENCES users(id),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_compliance_exemptions_entity 
    ON compliance_exemptions (entity_type, entity_id, doc_type);
CREATE INDEX IF NOT EXISTS idx_compliance_checks_entity 
    ON compliance_checks (entity_type, entity_id, check_type, created_at);
CREATE TABLE files_new (
    id              TEXT PRIMARY KEY, 
    filename        TEXT NOT NULL, 
    original_name   TEXT NOT NULL,
    path            TEXT NOT NULL, 
    size            INTEGER NOT NULL, 
    mime_type       TEXT NOT NULL,
    uploadable_type TEXT NOT NULL CHECK (uploadable_type IN 
        ('driver_license','vehicle_insurance','vehicle_permit','company_logo',
         'vehicle_rc','vehicle_fitness','vehicle_puc')),
    uploadable_id   TEXT, 
    created_at      TIMESTAMPTZ NOT NULL DEFAULT (CURRENT_TIMESTAMP)
);
INSERT INTO files_new (id, filename, original_name, path, size, mime_type, uploadable_type, uploadable_id, created_at)
SELECT id, filename, original_name, path, size, mime_type, uploadable_type, uploadable_id, created_at FROM files;
DROP TABLE files;
ALTER TABLE files_new RENAME TO files;
-- RBAC Seeds for Compliance (Spec 05 §11)
INSERT INTO permissions (name, description) VALUES
('compliance:read', 'View compliance checks and exemptions'),
('compliance:update', 'Create compliance exemptions and verify docs') ON CONFLICT DO NOTHING;
-- Admin (role 1) gets all
INSERT INTO role_permissions (role_id, permission_id)
SELECT 1, id FROM permissions WHERE name LIKE 'compliance:%' ON CONFLICT DO NOTHING;
-- Dispatcher (role 2) gets read
INSERT INTO role_permissions (role_id, permission_id)
SELECT 2, id FROM permissions WHERE name IN ('compliance:read') ON CONFLICT DO NOTHING;
-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN (SELECT id FROM permissions WHERE name LIKE 'compliance:%');
DELETE FROM permissions WHERE name LIKE 'compliance:%';
CREATE TABLE files_old (
    id              TEXT PRIMARY KEY, 
    filename        TEXT NOT NULL, 
    original_name   TEXT NOT NULL,
    path            TEXT NOT NULL, 
    size            INTEGER NOT NULL, 
    mime_type       TEXT NOT NULL,
    uploadable_type TEXT NOT NULL CHECK (uploadable_type IN 
        ('driver_license','vehicle_insurance','vehicle_permit','company_logo')),
    uploadable_id   TEXT, 
    created_at      TIMESTAMPTZ NOT NULL DEFAULT (CURRENT_TIMESTAMP)
);
INSERT INTO files_old (id, filename, original_name, path, size, mime_type, uploadable_type, uploadable_id, created_at)
SELECT id, filename, original_name, path, size, mime_type, uploadable_type, uploadable_id, created_at FROM files 
WHERE uploadable_type IN ('driver_license','vehicle_insurance','vehicle_permit','company_logo');
DROP TABLE files;
ALTER TABLE files_old RENAME TO files;
DROP INDEX IF EXISTS idx_compliance_checks_entity;
DROP INDEX IF EXISTS idx_compliance_exemptions_entity;
DROP TABLE IF EXISTS compliance_exemptions;
ALTER TABLE vehicles DROP COLUMN puc_expiry;
