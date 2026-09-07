-- PG port of 00126_fleet_registry_sop_parity.sql | status: MANUAL | flags: MANUAL-REBUILD | reviewed: YES
-- Native PG port: sqlite rebuilds vehicles (no ALTER CONSTRAINT support); PG
-- uses ADD/DROP COLUMN + DROP/ADD CONSTRAINT. Same end state, dependents kept.
-- PRAGMA lines dropped (sqlite-only). Tenant triggers use the shared
-- tenant_fk_guard_fn() created in the 00103 port (re-emitted OR REPLACE here
-- so this file is self-contained).
-- +goose Up
ALTER TABLE vehicles
    ADD COLUMN fleet_class TEXT NOT NULL DEFAULT 'CV' CHECK (fleet_class IN ('CV', 'PV', 'FS', 'OFS')),
    ADD COLUMN ownership TEXT NOT NULL DEFAULT 'O' CHECK (ownership IN ('O', 'C', 'F', 'M')),
    ADD COLUMN fleet_number TEXT,
    ADD COLUMN description TEXT,
    ADD COLUMN manufacturer TEXT,
    ADD COLUMN manuf_country TEXT,
    ADD COLUMN model TEXT,
    ADD COLUMN constr_year_month TEXT,
    ADD COLUMN acquisition_value REAL,
    ADD COLUMN acquisition_currency TEXT NOT NULL DEFAULT 'INR',
    ADD COLUMN acquisition_date DATE,
    ADD COLUMN purchase_vendor TEXT,
    ADD COLUMN valid_from DATE,
    ADD COLUMN valid_to DATE,
    ADD COLUMN facility_id TEXT,
    ADD COLUMN maint_plant TEXT,
    ADD COLUMN planning_plant TEXT,
    ADD COLUMN company_code TEXT,
    ADD COLUMN business_area TEXT,
    ADD COLUMN cost_center TEXT,
    ADD COLUMN asset_no TEXT,
    ADD COLUMN fleet_object_no TEXT,
    ADD COLUMN chassis_no TEXT,
    ADD COLUMN vehicle_category TEXT,
    ADD COLUMN engine_number TEXT,
    ADD COLUMN engine_power TEXT,
    ADD COLUMN engine_capacity TEXT,
    ADD COLUMN cylinder_count INTEGER,
    ADD COLUMN max_speed REAL,
    ADD COLUMN weight REAL,
    ADD COLUMN weight_unit TEXT NOT NULL DEFAULT 'TO' CHECK (weight_unit IN ('TO', 'KG')),
    ADD COLUMN load_volume REAL,
    ADD COLUMN volume_unit TEXT,
    ADD COLUMN secondary_fuel TEXT,
    ADD COLUMN usage_indicator TEXT;
ALTER TABLE vehicles DROP CONSTRAINT vehicles_status_check;
ALTER TABLE vehicles ADD CONSTRAINT vehicles_status_check CHECK (status IN ('available', 'running', 'maintenance', 'inactive', 'blocked'));
CREATE INDEX IF NOT EXISTS idx_vehicles_tenant ON vehicles(tenant_id);
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION tenant_fk_guard_fn()
RETURNS trigger AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id) THEN
        RAISE EXCEPTION 'FK violation: tenants(id) missing for %.tenant_id', TG_TABLE_NAME;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
DROP TRIGGER IF EXISTS trg_vehicles_tenant_fk_insert ON vehicles;
CREATE TRIGGER trg_vehicles_tenant_fk_insert
BEFORE INSERT ON vehicles
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_vehicles_tenant_fk_update ON vehicles;
CREATE TRIGGER trg_vehicles_tenant_fk_update
BEFORE UPDATE OF tenant_id ON vehicles
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL) EXECUTE FUNCTION tenant_fk_guard_fn();
CREATE TABLE vehicle_measuring_points (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  vehicle_id TEXT NOT NULL REFERENCES vehicles(id),
  category TEXT NOT NULL DEFAULT 'M',
  kind TEXT NOT NULL CHECK (kind IN ('ODO', 'FUEL_TOPUP', 'PUMP')),
  meas_position TEXT NOT NULL DEFAULT 'DISTANCE',
  unit TEXT NOT NULL DEFAULT 'KM' CHECK (unit IN ('KM', 'L')),
  decimal_places INTEGER NOT NULL DEFAULT 0,
  annual_estimate REAL,
  count_backwards INTEGER NOT NULL DEFAULT 0,
  is_counter INTEGER NOT NULL DEFAULT 1,
  description TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_meas_points_tenant_vehicle ON vehicle_measuring_points(tenant_id, vehicle_id);
CREATE TABLE vehicle_measurements (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  point_id TEXT NOT NULL REFERENCES vehicle_measuring_points(id),
  doc_number TEXT,
  counter_reading REAL NOT NULL,
  difference_reading REAL,
  total_counter_reading REAL,
  measured_at TIMESTAMPTZ,
  read_by TEXT,
  remarks TEXT,
  recorded_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  recorded_by TEXT
);
CREATE INDEX idx_measurements_point_time ON vehicle_measurements(point_id, recorded_at);
DROP TRIGGER IF EXISTS trg_meas_points_tenant_fk_insert ON vehicle_measuring_points;
CREATE TRIGGER trg_meas_points_tenant_fk_insert
BEFORE INSERT ON vehicle_measuring_points
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_meas_points_tenant_fk_update ON vehicle_measuring_points;
CREATE TRIGGER trg_meas_points_tenant_fk_update
BEFORE UPDATE OF tenant_id ON vehicle_measuring_points
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_measurements_tenant_fk_insert ON vehicle_measurements;
CREATE TRIGGER trg_measurements_tenant_fk_insert
BEFORE INSERT ON vehicle_measurements
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_measurements_tenant_fk_update ON vehicle_measurements;
CREATE TRIGGER trg_measurements_tenant_fk_update
BEFORE UPDATE OF tenant_id ON vehicle_measurements
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
-- +goose Down
-- Map 'blocked' back (old CHECK rejects it) before restoring the legacy CHECK.
UPDATE vehicles SET status = 'inactive' WHERE status = 'blocked';
DROP TRIGGER IF EXISTS trg_measurements_tenant_fk_update ON vehicle_measurements;
DROP TRIGGER IF EXISTS trg_measurements_tenant_fk_insert ON vehicle_measurements;
DROP TRIGGER IF EXISTS trg_meas_points_tenant_fk_update ON vehicle_measuring_points;
DROP TRIGGER IF EXISTS trg_meas_points_tenant_fk_insert ON vehicle_measuring_points;
DROP TABLE IF EXISTS vehicle_measurements;
DROP TABLE IF EXISTS vehicle_measuring_points;
-- NOTE: no rebuild, so the vehicles tenant triggers survive — no recreation needed
-- (sqlite Down recreates them only because DROP TABLE destroys them there).
ALTER TABLE vehicles DROP CONSTRAINT vehicles_status_check;
ALTER TABLE vehicles ADD CONSTRAINT vehicles_status_check CHECK (status IN ('available', 'running', 'maintenance', 'inactive'));
ALTER TABLE vehicles
    DROP COLUMN usage_indicator,
    DROP COLUMN secondary_fuel,
    DROP COLUMN volume_unit,
    DROP COLUMN load_volume,
    DROP COLUMN weight_unit,
    DROP COLUMN weight,
    DROP COLUMN max_speed,
    DROP COLUMN cylinder_count,
    DROP COLUMN engine_capacity,
    DROP COLUMN engine_power,
    DROP COLUMN engine_number,
    DROP COLUMN vehicle_category,
    DROP COLUMN chassis_no,
    DROP COLUMN fleet_object_no,
    DROP COLUMN asset_no,
    DROP COLUMN cost_center,
    DROP COLUMN business_area,
    DROP COLUMN company_code,
    DROP COLUMN planning_plant,
    DROP COLUMN maint_plant,
    DROP COLUMN facility_id,
    DROP COLUMN valid_to,
    DROP COLUMN valid_from,
    DROP COLUMN purchase_vendor,
    DROP COLUMN acquisition_date,
    DROP COLUMN acquisition_currency,
    DROP COLUMN acquisition_value,
    DROP COLUMN constr_year_month,
    DROP COLUMN model,
    DROP COLUMN manuf_country,
    DROP COLUMN manufacturer,
    DROP COLUMN description,
    DROP COLUMN fleet_number,
    DROP COLUMN ownership,
    DROP COLUMN fleet_class;
