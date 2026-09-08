-- +goose Up
-- +goose NO TRANSACTION
-- 00132 — vehicles.insurance_expiry / fitness_expiry / permit_expiry go
-- nullable (unknown doc date is NULL, never an invented +1y placeholder).
-- Registration side-effect flows (self-register, wizard first vehicle, driver
-- vehicle-link, reviewer claim auto-create) insert NULLs; the web/mobile
-- vehicle forms keep requiring real dates (parseRequiredDate/parseAPIDate).
-- SQLite cannot ALTER nullability: rebuild pattern per 00081/00126/00131.
-- Column ORDER matches the live DDL of 00126 for the explicit INSERT...SELECT.
-- NO BACKFILL: a fabricated +1y date is indistinguishable from a real one,
-- so legacy rows keep their values; only new rows are honest. All dispatch
-- and dashboard readers are IsZero()/Valid-guarded: NULL behaves exactly
-- like the old far-future date (passes), minus the lie in prod records.
--
-- NO TRANSACTION + PRAGMA foreign_keys=OFF is REQUIRED here (not optional):
-- vehicles is a referenced parent and app-wide FK enforcement is ON.

PRAGMA foreign_keys=OFF;

CREATE TABLE vehicles_rebuild_00132 (
    id                TEXT PRIMARY KEY,
    registration_number TEXT NOT NULL UNIQUE,
    vehicle_number    TEXT NOT NULL,
    vehicle_type      TEXT NOT NULL CHECK (vehicle_type IN ('truck', 'mini_truck', 'bus', 'van', 'pickup', 'tempo')),
    capacity          INTEGER NOT NULL,
    fuel_type         TEXT NOT NULL DEFAULT 'diesel' CHECK (fuel_type IN ('diesel', 'petrol', 'gas', 'electric', 'cng')),
    insurance_expiry  DATE,
    fitness_expiry    DATE,
    permit_expiry     DATE,
    status            TEXT NOT NULL DEFAULT 'available' CHECK (status IN ('available', 'running', 'maintenance', 'inactive', 'blocked')),
    current_mileage   REAL,
    created_at        DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at        DATETIME NOT NULL DEFAULT (datetime('now')),
    tenant_id TEXT DEFAULT '1' NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    blocked INTEGER NOT NULL DEFAULT 0,
    blocked_reason TEXT,
    rc_expiry DATE,
    odometer REAL NOT NULL DEFAULT 0.0,
    tank_capacity_litres REAL,
    fuel_sensor_fitted INTEGER NOT NULL DEFAULT 0,
    maintenance_due DATE,
    maintenance_override_by TEXT,
    maintenance_override_at DATETIME,
    maintenance_override_reason TEXT,
    puc_expiry DATE,
    fleet_class TEXT NOT NULL DEFAULT 'CV' CHECK (fleet_class IN ('CV', 'PV', 'FS', 'OFS')),
    ownership TEXT NOT NULL DEFAULT 'O' CHECK (ownership IN ('O', 'C', 'F', 'M')),
    fleet_number TEXT,
    description TEXT,
    manufacturer TEXT,
    manuf_country TEXT,
    model TEXT,
    constr_year_month TEXT,
    acquisition_value REAL,
    acquisition_currency TEXT NOT NULL DEFAULT 'INR',
    acquisition_date DATE,
    purchase_vendor TEXT,
    valid_from DATE,
    valid_to DATE,
    facility_id TEXT,
    maint_plant TEXT,
    planning_plant TEXT,
    company_code TEXT,
    business_area TEXT,
    cost_center TEXT,
    asset_no TEXT,
    fleet_object_no TEXT,
    chassis_no TEXT,
    vehicle_category TEXT,
    engine_number TEXT,
    engine_power TEXT,
    engine_capacity TEXT,
    cylinder_count INTEGER,
    max_speed REAL,
    weight REAL,
    weight_unit TEXT NOT NULL DEFAULT 'TO' CHECK (weight_unit IN ('TO', 'KG')),
    load_volume REAL,
    volume_unit TEXT,
    secondary_fuel TEXT,
    usage_indicator TEXT
);

INSERT INTO vehicles_rebuild_00132 (id, registration_number, vehicle_number, vehicle_type, capacity, fuel_type, insurance_expiry, fitness_expiry, permit_expiry, status, current_mileage, created_at, updated_at, tenant_id, version, blocked, blocked_reason, rc_expiry, odometer, tank_capacity_litres, fuel_sensor_fitted, maintenance_due, maintenance_override_by, maintenance_override_at, maintenance_override_reason, puc_expiry, fleet_class, ownership, fleet_number, description, manufacturer, manuf_country, model, constr_year_month, acquisition_value, acquisition_currency, acquisition_date, purchase_vendor, valid_from, valid_to, facility_id, maint_plant, planning_plant, company_code, business_area, cost_center, asset_no, fleet_object_no, chassis_no, vehicle_category, engine_number, engine_power, engine_capacity, cylinder_count, max_speed, weight, weight_unit, load_volume, volume_unit, secondary_fuel, usage_indicator)
SELECT id, registration_number, vehicle_number, vehicle_type, capacity, fuel_type, insurance_expiry, fitness_expiry, permit_expiry, status, current_mileage, created_at, updated_at, tenant_id, version, blocked, blocked_reason, rc_expiry, odometer, tank_capacity_litres, fuel_sensor_fitted, maintenance_due, maintenance_override_by, maintenance_override_at, maintenance_override_reason, puc_expiry, fleet_class, ownership, fleet_number, description, manufacturer, manuf_country, model, constr_year_month, acquisition_value, acquisition_currency, acquisition_date, purchase_vendor, valid_from, valid_to, facility_id, maint_plant, planning_plant, company_code, business_area, cost_center, asset_no, fleet_object_no, chassis_no, vehicle_category, engine_number, engine_power, engine_capacity, cylinder_count, max_speed, weight, weight_unit, load_volume, volume_unit, secondary_fuel, usage_indicator FROM vehicles;
DROP TABLE vehicles;
ALTER TABLE vehicles_rebuild_00132 RENAME TO vehicles;

CREATE INDEX idx_vehicles_tenant ON vehicles(tenant_id);

-- +goose StatementBegin
CREATE TRIGGER trg_vehicles_tenant_fk_insert
BEFORE INSERT ON vehicles
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for vehicles.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_vehicles_tenant_fk_update
BEFORE UPDATE OF tenant_id ON vehicles
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for vehicles.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose Down
-- +goose NO TRANSACTION
-- Restore NOT NULL. Rows left without doc dates regain a +1y value so the
-- copy back cannot violate the constraint (best-effort legacy pattern; the
-- fabrication this migration removes is knowingly reintroduced on downgrade).
-- Same NO TRANSACTION rationale as Up: vehicles is a referenced parent.
PRAGMA foreign_keys=OFF;
UPDATE vehicles SET insurance_expiry = date('now','+1 year') WHERE insurance_expiry IS NULL;
UPDATE vehicles SET fitness_expiry = date('now','+1 year') WHERE fitness_expiry IS NULL;
UPDATE vehicles SET permit_expiry = date('now','+1 year') WHERE permit_expiry IS NULL;

ALTER TABLE vehicles RENAME TO vehicles_down_00132;
CREATE TABLE vehicles (
    id                TEXT PRIMARY KEY,
    registration_number TEXT NOT NULL UNIQUE,
    vehicle_number    TEXT NOT NULL,
    vehicle_type      TEXT NOT NULL CHECK (vehicle_type IN ('truck', 'mini_truck', 'bus', 'van', 'pickup', 'tempo')),
    capacity          INTEGER NOT NULL,
    fuel_type         TEXT NOT NULL DEFAULT 'diesel' CHECK (fuel_type IN ('diesel', 'petrol', 'gas', 'electric', 'cng')),
    insurance_expiry  DATE NOT NULL,
    fitness_expiry    DATE NOT NULL,
    permit_expiry     DATE NOT NULL,
    status            TEXT NOT NULL DEFAULT 'available' CHECK (status IN ('available', 'running', 'maintenance', 'inactive', 'blocked')),
    current_mileage   REAL,
    created_at        DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at        DATETIME NOT NULL DEFAULT (datetime('now')),
    tenant_id TEXT DEFAULT '1' NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    blocked INTEGER NOT NULL DEFAULT 0,
    blocked_reason TEXT,
    rc_expiry DATE,
    odometer REAL NOT NULL DEFAULT 0.0,
    tank_capacity_litres REAL,
    fuel_sensor_fitted INTEGER NOT NULL DEFAULT 0,
    maintenance_due DATE,
    maintenance_override_by TEXT,
    maintenance_override_at DATETIME,
    maintenance_override_reason TEXT,
    puc_expiry DATE,
    fleet_class TEXT NOT NULL DEFAULT 'CV' CHECK (fleet_class IN ('CV', 'PV', 'FS', 'OFS')),
    ownership TEXT NOT NULL DEFAULT 'O' CHECK (ownership IN ('O', 'C', 'F', 'M')),
    fleet_number TEXT,
    description TEXT,
    manufacturer TEXT,
    manuf_country TEXT,
    model TEXT,
    constr_year_month TEXT,
    acquisition_value REAL,
    acquisition_currency TEXT NOT NULL DEFAULT 'INR',
    acquisition_date DATE,
    purchase_vendor TEXT,
    valid_from DATE,
    valid_to DATE,
    facility_id TEXT,
    maint_plant TEXT,
    planning_plant TEXT,
    company_code TEXT,
    business_area TEXT,
    cost_center TEXT,
    asset_no TEXT,
    fleet_object_no TEXT,
    chassis_no TEXT,
    vehicle_category TEXT,
    engine_number TEXT,
    engine_power TEXT,
    engine_capacity TEXT,
    cylinder_count INTEGER,
    max_speed REAL,
    weight REAL,
    weight_unit TEXT NOT NULL DEFAULT 'TO' CHECK (weight_unit IN ('TO', 'KG')),
    load_volume REAL,
    volume_unit TEXT,
    secondary_fuel TEXT,
    usage_indicator TEXT
);

INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, fuel_type, insurance_expiry, fitness_expiry, permit_expiry, status, current_mileage, created_at, updated_at, tenant_id, version, blocked, blocked_reason, rc_expiry, odometer, tank_capacity_litres, fuel_sensor_fitted, maintenance_due, maintenance_override_by, maintenance_override_at, maintenance_override_reason, puc_expiry, fleet_class, ownership, fleet_number, description, manufacturer, manuf_country, model, constr_year_month, acquisition_value, acquisition_currency, acquisition_date, purchase_vendor, valid_from, valid_to, facility_id, maint_plant, planning_plant, company_code, business_area, cost_center, asset_no, fleet_object_no, chassis_no, vehicle_category, engine_number, engine_power, engine_capacity, cylinder_count, max_speed, weight, weight_unit, load_volume, volume_unit, secondary_fuel, usage_indicator)
SELECT id, registration_number, vehicle_number, vehicle_type, capacity, fuel_type, insurance_expiry, fitness_expiry, permit_expiry, status, current_mileage, created_at, updated_at, tenant_id, version, blocked, blocked_reason, rc_expiry, odometer, tank_capacity_litres, fuel_sensor_fitted, maintenance_due, maintenance_override_by, maintenance_override_at, maintenance_override_reason, puc_expiry, fleet_class, ownership, fleet_number, description, manufacturer, manuf_country, model, constr_year_month, acquisition_value, acquisition_currency, acquisition_date, purchase_vendor, valid_from, valid_to, facility_id, maint_plant, planning_plant, company_code, business_area, cost_center, asset_no, fleet_object_no, chassis_no, vehicle_category, engine_number, engine_power, engine_capacity, cylinder_count, max_speed, weight, weight_unit, load_volume, volume_unit, secondary_fuel, usage_indicator FROM vehicles_down_00132;
DROP TABLE vehicles_down_00132;

CREATE INDEX idx_vehicles_tenant ON vehicles(tenant_id);

-- +goose StatementBegin
CREATE TRIGGER trg_vehicles_tenant_fk_insert
BEFORE INSERT ON vehicles
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for vehicles.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_vehicles_tenant_fk_update
BEFORE UPDATE OF tenant_id ON vehicles
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for vehicles.tenant_id') END;
END;
-- +goose StatementEnd
