-- PG port of 00166_driver_vehicle_assignments.sql | status: PORTABLE | flags: none
-- +goose Up
CREATE TABLE IF NOT EXISTS driver_preferred_vehicles (
    id            TEXT PRIMARY KEY,
    tenant_id     TEXT NOT NULL DEFAULT '1',
    driver_id     TEXT NOT NULL REFERENCES drivers(id) ON DELETE CASCADE,
    vehicle_id    TEXT NOT NULL REFERENCES vehicles(id) ON DELETE CASCADE,
    is_primary    INTEGER NOT NULL DEFAULT 1 CHECK (is_primary IN (0,1)),
    assigned_at   TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    unassigned_at TIMESTAMPTZ,
    assigned_by   TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
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

-- +goose Down
DROP INDEX IF EXISTS idx_dpv_tenant_vehicle_history;
DROP INDEX IF EXISTS idx_dpv_tenant_driver_history;
DROP INDEX IF EXISTS idx_dpv_tenant_vehicle_active;
DROP INDEX IF EXISTS idx_dpv_tenant_driver_active;
DROP TABLE IF EXISTS driver_preferred_vehicles;
