-- PG port of 00145_facilities_master.sql | status: PORTABLE | flags: none
-- +goose Up
CREATE TABLE IF NOT EXISTS facilities (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    facility_code   TEXT NOT NULL,
    name            TEXT NOT NULL,
    facility_type   TEXT NOT NULL DEFAULT 'depot'
                    CHECK (facility_type IN ('depot', 'hub', 'branch', 'workshop', 'office', 'fuel_station')),
    plant           TEXT NOT NULL DEFAULT '',
    circle          TEXT NOT NULL DEFAULT '',
    profit_center   TEXT NOT NULL DEFAULT '',
    cost_center     TEXT NOT NULL DEFAULT '',
    address         TEXT NOT NULL DEFAULT '',
    city            TEXT NOT NULL DEFAULT '',
    state           TEXT NOT NULL DEFAULT '',
    pincode         TEXT NOT NULL DEFAULT '',
    latitude        DOUBLE PRECISION,
    longitude       DOUBLE PRECISION,
    valid_from      DATE,
    valid_to        DATE,
    is_active       BOOLEAN NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_facilities_tenant_code UNIQUE (tenant_id, facility_code)
);

CREATE INDEX IF NOT EXISTS idx_facilities_tenant_code ON facilities(tenant_id, facility_code);
CREATE INDEX IF NOT EXISTS idx_facilities_tenant_type ON facilities(tenant_id, facility_type);
CREATE INDEX IF NOT EXISTS idx_facilities_tenant_active ON facilities(tenant_id, is_active);

DROP TRIGGER IF EXISTS trg_facilities_tenant_fk_insert ON facilities;
CREATE TRIGGER trg_facilities_tenant_fk_insert BEFORE INSERT ON facilities
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();

DROP TRIGGER IF EXISTS trg_facilities_tenant_fk_update ON facilities;
CREATE TRIGGER trg_facilities_tenant_fk_update BEFORE UPDATE OF tenant_id ON facilities
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();

INSERT INTO permissions (name, description) VALUES
('facilities:read', 'View facility master catalog and details'),
('facilities:write', 'Create and update facility master catalog records')
ON CONFLICT (name) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT 1, id FROM permissions WHERE name IN ('facilities:read', 'facilities:write')
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT 6, id FROM permissions WHERE name IN ('facilities:read', 'facilities:write')
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT 2, id FROM permissions WHERE name IN ('facilities:read')
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN (SELECT id FROM permissions WHERE name IN ('facilities:read', 'facilities:write'));
DELETE FROM permissions WHERE name IN ('facilities:read', 'facilities:write');
DROP TRIGGER IF EXISTS trg_facilities_tenant_fk_update ON facilities;
DROP TRIGGER IF EXISTS trg_facilities_tenant_fk_insert ON facilities;
DROP INDEX IF EXISTS idx_facilities_tenant_active;
DROP INDEX IF EXISTS idx_facilities_tenant_type;
DROP INDEX IF EXISTS idx_facilities_tenant_code;
DROP TABLE IF EXISTS facilities CASCADE;
