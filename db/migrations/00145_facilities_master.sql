-- +goose Up
-- 00145: facilities_master — SAP ZFID facility master catalog (TMS_SOP pp.1-2, B6).
-- Physical depots, hubs, branches, workshops, and fuel stations keyed by facility_code (e.g. MM21000000757).

CREATE TABLE IF NOT EXISTS facilities (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL,
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
    latitude        REAL,
    longitude       REAL,
    valid_from      DATE,
    valid_to        DATE,
    is_active       INTEGER NOT NULL DEFAULT 1,
    created_at      DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at      DATETIME NOT NULL DEFAULT (datetime('now')),
    UNIQUE (tenant_id, facility_code)
);

CREATE INDEX IF NOT EXISTS idx_facilities_tenant_code
    ON facilities(tenant_id, facility_code);
CREATE INDEX IF NOT EXISTS idx_facilities_tenant_type
    ON facilities(tenant_id, facility_type);
CREATE INDEX IF NOT EXISTS idx_facilities_tenant_active
    ON facilities(tenant_id, is_active);

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_facilities_tenant_fk_insert
BEFORE INSERT ON facilities
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for facilities.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_facilities_tenant_fk_update
BEFORE UPDATE OF tenant_id ON facilities
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for facilities.tenant_id') END;
END;
-- +goose StatementEnd

INSERT OR IGNORE INTO permissions (name, description) VALUES
('facilities:read', 'View facility master catalog and details'),
('facilities:write', 'Create and update facility master catalog records');

INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT 1, id FROM permissions WHERE name IN ('facilities:read', 'facilities:write');

INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT 6, id FROM permissions WHERE name IN ('facilities:read', 'facilities:write');

INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT 2, id FROM permissions WHERE name IN ('facilities:read');

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN (SELECT id FROM permissions WHERE name IN ('facilities:read', 'facilities:write'));
DELETE FROM permissions WHERE name IN ('facilities:read', 'facilities:write');
DROP TRIGGER IF EXISTS trg_facilities_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_facilities_tenant_fk_insert;
DROP INDEX IF EXISTS idx_facilities_tenant_active;
DROP INDEX IF EXISTS idx_facilities_tenant_type;
DROP INDEX IF EXISTS idx_facilities_tenant_code;
DROP TABLE IF EXISTS facilities;
