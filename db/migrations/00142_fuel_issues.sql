-- +goose Up
-- 00142: fuel_issues — Fuel issue entry with pump OP/CL readings (TMS_SOP p.9 parity, B2).
-- Fuel station (fleet_class 'FS'/'OFS') issues fuel to vehicles via calibrated pump points.
-- Captures opening/closing pump readings, litres issued, odometer reading, and cost.

CREATE TABLE IF NOT EXISTS fuel_issues (
    id                TEXT PRIMARY KEY,
    tenant_id         TEXT NOT NULL,
    issue_number      TEXT,
    fuel_station_id   TEXT NOT NULL,
    pump_point_id     TEXT,
    vehicle_id        TEXT NOT NULL,
    driver_id         TEXT,
    trip_id           TEXT,
    fuel_type         TEXT NOT NULL DEFAULT 'diesel',
    opening_reading   REAL NOT NULL DEFAULT 0,
    closing_reading   REAL NOT NULL DEFAULT 0,
    litres_issued     REAL NOT NULL CHECK (litres_issued > 0),
    vehicle_odometer  REAL,
    rate_per_litre    REAL,
    total_cost        REAL,
    remarks           TEXT NOT NULL DEFAULT '',
    issued_at         DATETIME NOT NULL DEFAULT (datetime('now')),
    created_by        TEXT NOT NULL DEFAULT '',
    created_at        DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at        DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (fuel_station_id) REFERENCES vehicles(id),
    FOREIGN KEY (pump_point_id) REFERENCES vehicle_measuring_points(id),
    FOREIGN KEY (vehicle_id) REFERENCES vehicles(id),
    FOREIGN KEY (driver_id) REFERENCES drivers(id),
    FOREIGN KEY (trip_id) REFERENCES trips(id)
);

CREATE INDEX IF NOT EXISTS idx_fuel_issues_tenant_time
    ON fuel_issues(tenant_id, issued_at DESC);
CREATE INDEX IF NOT EXISTS idx_fuel_issues_vehicle
    ON fuel_issues(tenant_id, vehicle_id, issued_at DESC);
CREATE INDEX IF NOT EXISTS idx_fuel_issues_station
    ON fuel_issues(tenant_id, fuel_station_id, issued_at DESC);

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_fuel_issues_tenant_fk_insert
BEFORE INSERT ON fuel_issues
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for fuel_issues.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_fuel_issues_tenant_fk_update
BEFORE UPDATE OF tenant_id ON fuel_issues
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for fuel_issues.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS trg_fuel_issues_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_fuel_issues_tenant_fk_insert;
DROP INDEX IF EXISTS idx_fuel_issues_station;
DROP INDEX IF EXISTS idx_fuel_issues_vehicle;
DROP INDEX IF EXISTS idx_fuel_issues_tenant_time;
DROP TABLE IF EXISTS fuel_issues;
