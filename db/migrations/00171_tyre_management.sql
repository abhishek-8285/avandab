-- +goose Up
-- 00171: Tyre management (FleetX Parity P3.1)
-- Introduces tyre master, mounting/axle position tracking, CPKM vs GPS odometer,
-- inspection tread depth history, retreading lifecycle, and scrap management.

CREATE TABLE IF NOT EXISTS tyres (
    id                     TEXT PRIMARY KEY,
    tenant_id              TEXT NOT NULL,
    serial_number          TEXT NOT NULL,
    brand                  TEXT NOT NULL,
    model                  TEXT NOT NULL DEFAULT '',
    size                   TEXT NOT NULL,
    tyre_type              TEXT NOT NULL DEFAULT 'TUBELESS' CHECK (tyre_type IN ('TUBELESS', 'TUBE_TYPE', 'RADIAL', 'NYLON')),
    status                 TEXT NOT NULL DEFAULT 'IN_STOCK' CHECK (status IN ('IN_STOCK', 'MOUNTED', 'UNDER_RETREAD', 'SCRAPPED')),
    vehicle_id             TEXT,
    axle_position          TEXT,
    mount_odometer_km      REAL,
    mount_date             DATETIME,
    initial_tread_depth_mm REAL NOT NULL DEFAULT 15.0,
    current_tread_depth_mm REAL NOT NULL DEFAULT 15.0,
    min_tread_depth_mm     REAL NOT NULL DEFAULT 3.0,
    purchase_date          DATE,
    purchase_cost          REAL NOT NULL DEFAULT 0.0,
    vendor_name            TEXT NOT NULL DEFAULT '',
    invoice_number         TEXT NOT NULL DEFAULT '',
    retread_count          INTEGER NOT NULL DEFAULT 0,
    max_retreads           INTEGER NOT NULL DEFAULT 3,
    total_retread_cost     REAL NOT NULL DEFAULT 0.0,
    total_km_run           REAL NOT NULL DEFAULT 0.0,
    scrap_date             DATETIME,
    scrap_reason           TEXT NOT NULL DEFAULT '',
    scrap_recovery_amount  REAL NOT NULL DEFAULT 0.0,
    created_at             DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at             DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    FOREIGN KEY (vehicle_id) REFERENCES vehicles(id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_tyres_tenant_serial
    ON tyres(tenant_id, serial_number);

CREATE INDEX IF NOT EXISTS idx_tyres_tenant_status
    ON tyres(tenant_id, status);

CREATE INDEX IF NOT EXISTS idx_tyres_tenant_vehicle
    ON tyres(tenant_id, vehicle_id);

CREATE UNIQUE INDEX IF NOT EXISTS idx_tyres_vehicle_axle_mounted
    ON tyres(vehicle_id, axle_position)
    WHERE status = 'MOUNTED' AND vehicle_id IS NOT NULL AND axle_position IS NOT NULL;

CREATE TABLE IF NOT EXISTS tyre_activities (
    id             TEXT PRIMARY KEY,
    tenant_id      TEXT NOT NULL,
    tyre_id        TEXT NOT NULL,
    activity_type  TEXT NOT NULL CHECK (activity_type IN ('MOUNT', 'DISMOUNT', 'ROTATION', 'INSPECTION', 'RETREAD_SEND', 'RETREAD_RECEIVE', 'SCRAP')),
    vehicle_id     TEXT,
    from_position  TEXT,
    to_position    TEXT,
    odometer_km    REAL NOT NULL DEFAULT 0.0,
    tread_depth_mm REAL,
    pressure_psi   REAL,
    cost           REAL NOT NULL DEFAULT 0.0,
    notes          TEXT NOT NULL DEFAULT '',
    performed_by   TEXT NOT NULL DEFAULT '',
    recorded_at    DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    FOREIGN KEY (tyre_id) REFERENCES tyres(id),
    FOREIGN KEY (vehicle_id) REFERENCES vehicles(id)
);

CREATE INDEX IF NOT EXISTS idx_tyre_act_tyre
    ON tyre_activities(tyre_id, recorded_at DESC);

CREATE INDEX IF NOT EXISTS idx_tyre_act_tenant
    ON tyre_activities(tenant_id, recorded_at DESC);

CREATE TABLE IF NOT EXISTS tyre_inspections (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL,
    tyre_id         TEXT NOT NULL,
    vehicle_id      TEXT,
    inspection_date DATETIME NOT NULL DEFAULT (datetime('now')),
    odometer_km     REAL NOT NULL DEFAULT 0.0,
    tread_depth_mm  REAL NOT NULL,
    pressure_psi    REAL,
    condition       TEXT NOT NULL DEFAULT 'GOOD' CHECK (condition IN ('GOOD', 'UNEVEN_WEAR', 'CUTS_OR_CRACKS', 'BULGE', 'PUNCTURE', 'WORN_OUT')),
    action_taken    TEXT NOT NULL DEFAULT '',
    inspector_name  TEXT NOT NULL DEFAULT '',
    notes           TEXT NOT NULL DEFAULT '',
    created_at      DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    FOREIGN KEY (tyre_id) REFERENCES tyres(id),
    FOREIGN KEY (vehicle_id) REFERENCES vehicles(id)
);

CREATE INDEX IF NOT EXISTS idx_tyre_insp_tyre
    ON tyre_inspections(tyre_id, inspection_date DESC);

CREATE INDEX IF NOT EXISTS idx_tyre_insp_tenant
    ON tyre_inspections(tenant_id, inspection_date DESC);

-- Tenant isolation triggers (Spec 00103 rule)
-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_tyres_tenant_fk_insert
BEFORE INSERT ON tyres
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for tyres.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_tyres_tenant_fk_update
BEFORE UPDATE OF tenant_id ON tyres
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for tyres.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_tyre_activities_tenant_fk_insert
BEFORE INSERT ON tyre_activities
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for tyre_activities.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_tyre_activities_tenant_fk_update
BEFORE UPDATE OF tenant_id ON tyre_activities
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for tyre_activities.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_tyre_inspections_tenant_fk_insert
BEFORE INSERT ON tyre_inspections
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for tyre_inspections.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_tyre_inspections_tenant_fk_update
BEFORE UPDATE OF tenant_id ON tyre_inspections
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for tyre_inspections.tenant_id') END;
END;
-- +goose StatementEnd

-- RBAC Seeding
INSERT OR IGNORE INTO permissions (name, description) VALUES
    ('tyres:read', 'View tyres, axle positions, CPKM and inspections'),
    ('tyres:write', 'Manage tyres, mount, rotate, retread, scrap and record inspections');

-- Admin (1) gets full tyre capabilities
INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT 1, id FROM permissions WHERE name IN ('tyres:read', 'tyres:write');

-- Dispatcher (2) gets full tyre capabilities for fleet operations
INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT 2, id FROM permissions WHERE name IN ('tyres:read', 'tyres:write');

-- Org Admin (6) gets full tyre capabilities
INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT 6, id FROM permissions WHERE name IN ('tyres:read', 'tyres:write');

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN (SELECT id FROM permissions WHERE name IN ('tyres:read', 'tyres:write'));
DELETE FROM permissions WHERE name IN ('tyres:read', 'tyres:write');

DROP TRIGGER IF EXISTS trg_tyre_inspections_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_tyre_inspections_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_tyre_activities_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_tyre_activities_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_tyres_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_tyres_tenant_fk_insert;

DROP TABLE IF EXISTS tyre_inspections;
DROP TABLE IF EXISTS tyre_activities;
DROP TABLE IF EXISTS tyres;
