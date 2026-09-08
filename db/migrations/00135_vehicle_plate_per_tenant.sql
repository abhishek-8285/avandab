-- +goose Up
-- +goose NO TRANSACTION
-- 00135 — plates are per-tenant, not global (H3) + latest-position tenant
-- uniqueness (L10). registration_number was TEXT NOT NULL UNIQUE across ALL
-- tenants: tenant B could never register a plate tenant A held, and one
-- tenant could squat another's plates. Rebuild pattern per 00081/00126/00131.
-- DDL machine-generated from a scratch DB migrated 00001→00134, so 00132's
-- nullable expiries (and every other ALTER) survive the rebuild.
-- Data-safe by construction: the old global UNIQUE implies no same-tenant
-- dupes, so the new composite UNIQUE cannot fail on existing rows.
--
-- NO TRANSACTION + PRAGMA foreign_keys=OFF REQUIRED (not optional): vehicles
-- is a referenced parent (trips, telemetry, ownership) with FK enforcement
-- ON, so DROP TABLE vehicles inside a tx fails.

PRAGMA foreign_keys=OFF;

CREATE TABLE vehicles_rebuild_00135 (
    id                TEXT PRIMARY KEY,
    registration_number TEXT NOT NULL,
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
,
    UNIQUE (tenant_id, registration_number)
);

INSERT INTO vehicles_rebuild_00135 (id,registration_number,vehicle_number,vehicle_type,capacity,fuel_type,insurance_expiry,fitness_expiry,permit_expiry,status,current_mileage,created_at,updated_at,tenant_id,version,blocked,blocked_reason,rc_expiry,odometer,tank_capacity_litres,fuel_sensor_fitted,maintenance_due,maintenance_override_by,maintenance_override_at,maintenance_override_reason,puc_expiry,fleet_class,ownership,fleet_number,description,manufacturer,manuf_country,model,constr_year_month,acquisition_value,acquisition_currency,acquisition_date,purchase_vendor,valid_from,valid_to,facility_id,maint_plant,planning_plant,company_code,business_area,cost_center,asset_no,fleet_object_no,chassis_no,vehicle_category,engine_number,engine_power,engine_capacity,cylinder_count,max_speed,weight,weight_unit,load_volume,volume_unit,secondary_fuel,usage_indicator)
SELECT id,registration_number,vehicle_number,vehicle_type,capacity,fuel_type,insurance_expiry,fitness_expiry,permit_expiry,status,current_mileage,created_at,updated_at,tenant_id,version,blocked,blocked_reason,rc_expiry,odometer,tank_capacity_litres,fuel_sensor_fitted,maintenance_due,maintenance_override_by,maintenance_override_at,maintenance_override_reason,puc_expiry,fleet_class,ownership,fleet_number,description,manufacturer,manuf_country,model,constr_year_month,acquisition_value,acquisition_currency,acquisition_date,purchase_vendor,valid_from,valid_to,facility_id,maint_plant,planning_plant,company_code,business_area,cost_center,asset_no,fleet_object_no,chassis_no,vehicle_category,engine_number,engine_power,engine_capacity,cylinder_count,max_speed,weight,weight_unit,load_volume,volume_unit,secondary_fuel,usage_indicator FROM vehicles;
DROP TABLE vehicles;
ALTER TABLE vehicles_rebuild_00135 RENAME TO vehicles;

CREATE INDEX IF NOT EXISTS idx_vehicles_tenant ON vehicles(tenant_id);

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

-- L10: defense-in-depth tenant scope on the live-map cache. Safe today only
-- because vehicle IDs are UUIDs; the composite unique makes it structural.
-- Plain CREATE UNIQUE INDEX: no rebuild needed for an added constraint.
CREATE UNIQUE INDEX IF NOT EXISTS idx_vehicle_latest_position_tenant_vehicle
    ON vehicle_latest_position(tenant_id, vehicle_id);

-- +goose Down
-- +goose NO TRANSACTION
PRAGMA foreign_keys=OFF;

CREATE TABLE vehicles_down_00135 (
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

INSERT INTO vehicles_down_00135 (id,registration_number,vehicle_number,vehicle_type,capacity,fuel_type,insurance_expiry,fitness_expiry,permit_expiry,status,current_mileage,created_at,updated_at,tenant_id,version,blocked,blocked_reason,rc_expiry,odometer,tank_capacity_litres,fuel_sensor_fitted,maintenance_due,maintenance_override_by,maintenance_override_at,maintenance_override_reason,puc_expiry,fleet_class,ownership,fleet_number,description,manufacturer,manuf_country,model,constr_year_month,acquisition_value,acquisition_currency,acquisition_date,purchase_vendor,valid_from,valid_to,facility_id,maint_plant,planning_plant,company_code,business_area,cost_center,asset_no,fleet_object_no,chassis_no,vehicle_category,engine_number,engine_power,engine_capacity,cylinder_count,max_speed,weight,weight_unit,load_volume,volume_unit,secondary_fuel,usage_indicator)
SELECT id,registration_number,vehicle_number,vehicle_type,capacity,fuel_type,insurance_expiry,fitness_expiry,permit_expiry,status,current_mileage,created_at,updated_at,tenant_id,version,blocked,blocked_reason,rc_expiry,odometer,tank_capacity_litres,fuel_sensor_fitted,maintenance_due,maintenance_override_by,maintenance_override_at,maintenance_override_reason,puc_expiry,fleet_class,ownership,fleet_number,description,manufacturer,manuf_country,model,constr_year_month,acquisition_value,acquisition_currency,acquisition_date,purchase_vendor,valid_from,valid_to,facility_id,maint_plant,planning_plant,company_code,business_area,cost_center,asset_no,fleet_object_no,chassis_no,vehicle_category,engine_number,engine_power,engine_capacity,cylinder_count,max_speed,weight,weight_unit,load_volume,volume_unit,secondary_fuel,usage_indicator FROM vehicles;
DROP TABLE vehicles;
ALTER TABLE vehicles_down_00135 RENAME TO vehicles;

CREATE INDEX IF NOT EXISTS idx_vehicles_tenant ON vehicles(tenant_id);

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

DROP INDEX IF EXISTS idx_vehicle_latest_position_tenant_vehicle;
