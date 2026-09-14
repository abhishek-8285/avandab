-- +goose Up
-- 00154 — DPDP breach-notice ledger (Digital Personal Data Protection Act,
-- 2023 §8(6) + DPDP Rules: intimate the Board and each affected principal
-- without delay; file the detailed report within 72 hours of awareness).
-- detected_at is the awareness clock; the 72h due date derives from it in
-- queries (no stored due column to drift). Status is explicit:
-- open → notified (both stamps set) → detailed (report filed) → closed.

CREATE TABLE IF NOT EXISTS breach_incidents (
    id                      TEXT PRIMARY KEY,
    tenant_id               TEXT NOT NULL,
    title                   TEXT NOT NULL,
    description             TEXT NOT NULL DEFAULT '',
    nature                  TEXT NOT NULL DEFAULT '',
    extent                  TEXT NOT NULL DEFAULT '',
    affected_count          INTEGER NOT NULL DEFAULT 0,
    status                  TEXT NOT NULL DEFAULT 'open'
                            CHECK (status IN ('open', 'notified', 'detailed', 'closed')),
    detected_at             DATETIME NOT NULL DEFAULT (datetime('now')),
    board_notified_at       DATETIME,
    principals_notified_at  DATETIME,
    detail_report           TEXT NOT NULL DEFAULT '',
    detailed_at             DATETIME,
    created_at              DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at              DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_breach_incidents_tenant_status
    ON breach_incidents(tenant_id, status);

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_breach_incidents_tenant_fk_insert
BEFORE INSERT ON breach_incidents
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for breach_incidents.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_breach_incidents_tenant_fk_update
BEFORE UPDATE OF tenant_id ON breach_incidents
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for breach_incidents.tenant_id') END;
END;
-- +goose StatementEnd

-- Self-service consent needed no permission (session identity is the scope);
-- breach reporting administers the org, so it takes a real permission with
-- the 00150-mandated backfill (roles 1 + 6; exclusion list does not cover it).
INSERT OR IGNORE INTO permissions (name, description) VALUES
('privacy:manage', 'Report and manage data-breach incidents (DPDP §8(6))');

INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT 1, id FROM permissions WHERE name = 'privacy:manage';

INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT 6, id FROM permissions WHERE name = 'privacy:manage';

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN (SELECT id FROM permissions WHERE name = 'privacy:manage');
DELETE FROM permissions WHERE name = 'privacy:manage';
DROP TRIGGER IF EXISTS trg_breach_incidents_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_breach_incidents_tenant_fk_insert;
DROP INDEX IF EXISTS idx_breach_incidents_tenant_status;
DROP TABLE IF EXISTS breach_incidents;
