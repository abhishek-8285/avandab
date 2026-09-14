-- PG port of 00154_breach_incidents.sql | status: PORTABLE | flags: none
-- +goose Up
CREATE TABLE IF NOT EXISTS breach_incidents (
    id                      TEXT PRIMARY KEY,
    tenant_id               TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    title                   TEXT NOT NULL,
    description             TEXT NOT NULL DEFAULT '',
    nature                  TEXT NOT NULL DEFAULT '',
    extent                  TEXT NOT NULL DEFAULT '',
    affected_count          INTEGER NOT NULL DEFAULT 0,
    status                  TEXT NOT NULL DEFAULT 'open'
                            CHECK (status IN ('open', 'notified', 'detailed', 'closed')),
    detected_at             TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    board_notified_at       TIMESTAMPTZ,
    principals_notified_at  TIMESTAMPTZ,
    detail_report           TEXT NOT NULL DEFAULT '',
    detailed_at             TIMESTAMPTZ,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_breach_incidents_tenant_status ON breach_incidents(tenant_id, status);

DROP TRIGGER IF EXISTS trg_breach_incidents_tenant_fk_insert ON breach_incidents;
CREATE TRIGGER trg_breach_incidents_tenant_fk_insert BEFORE INSERT ON breach_incidents
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();

DROP TRIGGER IF EXISTS trg_breach_incidents_tenant_fk_update ON breach_incidents;
CREATE TRIGGER trg_breach_incidents_tenant_fk_update BEFORE UPDATE OF tenant_id ON breach_incidents
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();

INSERT INTO permissions (name, description) VALUES
('privacy:manage', 'Report and manage data-breach incidents (DPDP §8(6))')
ON CONFLICT (name) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT 1, id FROM permissions WHERE name = 'privacy:manage'
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT 6, id FROM permissions WHERE name = 'privacy:manage'
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN (SELECT id FROM permissions WHERE name = 'privacy:manage');
DELETE FROM permissions WHERE name = 'privacy:manage';
DROP TRIGGER IF EXISTS trg_breach_incidents_tenant_fk_update ON breach_incidents;
DROP TRIGGER IF EXISTS trg_breach_incidents_tenant_fk_insert ON breach_incidents;
DROP INDEX IF EXISTS idx_breach_incidents_tenant_status;
DROP TABLE IF EXISTS breach_incidents CASCADE;
