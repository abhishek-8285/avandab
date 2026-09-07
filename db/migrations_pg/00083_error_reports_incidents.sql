-- PG port of 00083_error_reports_incidents.sql | status: PORTABLE | flags: none
-- +goose Up
-- Spec 16 §3 (error_reports + incidents DDL) — carried over from 00058, which
-- shipped without these two tables despite the ownership index entry.
-- Fingerprint formula override: sha1(method + url + firstLine(message) + tenant_id)
-- per the §5.5 gap-table decision; UNIQUE(fingerprint, tenant_id) added to make
-- dedup an atomic upsert.
-- RBAC: errors:read / errors:update follow 00058's resource:action convention.

CREATE TABLE IF NOT EXISTS error_reports (
    id          TEXT PRIMARY KEY,
    fingerprint TEXT NOT NULL,
    tenant_id   TEXT NOT NULL DEFAULT '1',
    user_id     TEXT,
    url         TEXT NOT NULL DEFAULT '',
    method      TEXT NOT NULL DEFAULT '',
    status_code INTEGER NOT NULL DEFAULT 0,
    severity    TEXT NOT NULL DEFAULT 'MEDIUM',
    message     TEXT NOT NULL,
    stack_trace TEXT NOT NULL DEFAULT '',
    environment TEXT NOT NULL DEFAULT '',
    app_version TEXT NOT NULL DEFAULT '',
    occurrences INTEGER NOT NULL DEFAULT 1,
    first_seen  TEXT NOT NULL,
    last_seen   TEXT NOT NULL,
    created_at  TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_error_reports_fingerprint ON error_reports (fingerprint);
CREATE INDEX IF NOT EXISTS idx_error_reports_tenant_sev ON error_reports (tenant_id, severity, last_seen);
CREATE UNIQUE INDEX IF NOT EXISTS uq_error_reports_fp_tenant ON error_reports (fingerprint, tenant_id);
CREATE TABLE IF NOT EXISTS incidents (
    id          TEXT PRIMARY KEY,
    error_id    TEXT NOT NULL REFERENCES error_reports(id),
    tenant_id   TEXT NOT NULL DEFAULT '1',
    status      TEXT NOT NULL DEFAULT 'OPEN' CHECK (status IN ('OPEN','ASSIGNED','RESOLVED')),
    severity    TEXT NOT NULL,
    assigned_to TEXT NOT NULL DEFAULT '',
    root_cause  TEXT NOT NULL DEFAULT '',
    created     TEXT NOT NULL,
    resolved_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_incidents_status_tenant ON incidents (status, tenant_id);
INSERT INTO permissions (name, description) VALUES
    ('errors:read', 'View ops error reports and incidents'),
    ('errors:update', 'Resolve and assign ops incidents') ON CONFLICT DO NOTHING;
INSERT INTO role_permissions (role_id, permission_id)
SELECT 1, id FROM permissions WHERE name IN ('errors:read', 'errors:update') ON CONFLICT DO NOTHING;
INSERT INTO role_permissions (role_id, permission_id)
SELECT 2, id FROM permissions WHERE name IN ('errors:read') ON CONFLICT DO NOTHING;
-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN
(SELECT id FROM permissions WHERE name IN ('errors:read', 'errors:update'));
DELETE FROM permissions WHERE name IN ('errors:read', 'errors:update');
DROP TABLE IF EXISTS incidents;
DROP TABLE IF EXISTS error_reports;
