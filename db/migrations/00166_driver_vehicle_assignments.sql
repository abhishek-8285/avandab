-- +goose Up
-- 00166: Driver preferred vehicle (master/default Driver->Vehicle).
-- One active per driver AND per vehicle (tenant-scoped), history via
-- unassigned_at. Uses new table driver_preferred_vehicles to avoid
-- colliding with driver_vehicle_assignments from 00108 (different CHECK).

CREATE TABLE IF NOT EXISTS driver_preferred_vehicles (
    id            TEXT PRIMARY KEY,
    tenant_id     TEXT NOT NULL DEFAULT '1',
    driver_id     TEXT NOT NULL,
    vehicle_id    TEXT NOT NULL,
    is_primary    INTEGER NOT NULL DEFAULT 1 CHECK (is_primary IN (0,1)),
    assigned_at   DATETIME NOT NULL DEFAULT (datetime('now')),
    unassigned_at DATETIME,
    assigned_by   TEXT,
    created_at    DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at    DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (driver_id) REFERENCES drivers(id) ON DELETE CASCADE,
    FOREIGN KEY (vehicle_id) REFERENCES vehicles(id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_dpv_tenant_driver_active
    ON driver_preferred_vehicles(tenant_id, driver_id)
    WHERE unassigned_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_dpv_tenant_vehicle_active
    ON driver_preferred_vehicles(tenant_id, vehicle_id)
    WHERE unassigned_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_dpv_tenant_driver_history
    ON driver_preferred_vehicles(tenant_id, driver_id, assigned_at DESC);

CREATE INDEX IF NOT EXISTS idx_dpv_tenant_vehicle_history
    ON driver_preferred_vehicles(tenant_id, vehicle_id, assigned_at DESC);

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_dpv_tenant_fk_insert
BEFORE INSERT ON driver_preferred_vehicles
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for driver_preferred_vehicles.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_dpv_tenant_fk_update
BEFORE UPDATE OF tenant_id ON driver_preferred_vehicles
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for driver_preferred_vehicles.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS trg_dpv_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_dpv_tenant_fk_insert;
DROP INDEX IF EXISTS idx_dpv_tenant_vehicle_history;
DROP INDEX IF EXISTS idx_dpv_tenant_driver_history;
DROP INDEX IF EXISTS idx_dpv_tenant_vehicle_active;
DROP INDEX IF EXISTS idx_dpv_tenant_driver_active;
DROP TABLE IF EXISTS driver_preferred_vehicles;
