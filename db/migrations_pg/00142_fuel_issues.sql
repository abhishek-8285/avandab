-- PG port of 00142_fuel_issues.sql | status: PORTABLE | flags: none
-- +goose Up
CREATE TABLE IF NOT EXISTS fuel_issues (
    id                TEXT PRIMARY KEY,
    tenant_id         TEXT NOT NULL,
    issue_number      TEXT,
    fuel_station_id   TEXT NOT NULL REFERENCES vehicles(id),
    pump_point_id     TEXT REFERENCES vehicle_measuring_points(id),
    vehicle_id        TEXT NOT NULL REFERENCES vehicles(id),
    driver_id         TEXT REFERENCES drivers(id),
    trip_id           TEXT REFERENCES trips(id),
    fuel_type         TEXT NOT NULL DEFAULT 'diesel',
    opening_reading   DOUBLE PRECISION NOT NULL DEFAULT 0,
    closing_reading   DOUBLE PRECISION NOT NULL DEFAULT 0,
    litres_issued     DOUBLE PRECISION NOT NULL CHECK (litres_issued > 0),
    vehicle_odometer  DOUBLE PRECISION,
    rate_per_litre    DOUBLE PRECISION,
    total_cost        DOUBLE PRECISION,
    remarks           TEXT NOT NULL DEFAULT '',
    issued_at         TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by        TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_fuel_issues_tenant_time
    ON fuel_issues(tenant_id, issued_at DESC);
CREATE INDEX IF NOT EXISTS idx_fuel_issues_vehicle
    ON fuel_issues(tenant_id, vehicle_id, issued_at DESC);
CREATE INDEX IF NOT EXISTS idx_fuel_issues_station
    ON fuel_issues(tenant_id, fuel_station_id, issued_at DESC);

DROP TRIGGER IF EXISTS trg_fuel_issues_tenant_fk_insert ON fuel_issues;
CREATE TRIGGER trg_fuel_issues_tenant_fk_insert BEFORE INSERT ON fuel_issues
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();

DROP TRIGGER IF EXISTS trg_fuel_issues_tenant_fk_update ON fuel_issues;
CREATE TRIGGER trg_fuel_issues_tenant_fk_update BEFORE UPDATE OF tenant_id ON fuel_issues
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();

-- +goose Down
DROP TRIGGER IF EXISTS trg_fuel_issues_tenant_fk_update ON fuel_issues;
DROP TRIGGER IF EXISTS trg_fuel_issues_tenant_fk_insert ON fuel_issues;
DROP INDEX IF EXISTS idx_fuel_issues_station;
DROP INDEX IF EXISTS idx_fuel_issues_vehicle;
DROP INDEX IF EXISTS idx_fuel_issues_tenant_time;
DROP TABLE IF EXISTS fuel_issues;
