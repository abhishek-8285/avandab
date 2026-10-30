-- +goose Up
-- PG port of 00177_cold_chain.sql
-- Cold Chain Telematics & HACCP/Pharma Temperature Excursions (FleetX Parity P3 3.11)
-- Multi-zone reefer monitoring, set-point drift tracking, door breach logging,
-- and GDP/pharma Mean Kinetic Temperature (MKT) compliance audit trails.

CREATE TABLE IF NOT EXISTS vehicle_reefer_profiles (
    vehicle_id                TEXT PRIMARY KEY REFERENCES vehicles(id) ON DELETE CASCADE,
    tenant_id                 TEXT NOT NULL REFERENCES tenants(id),
    reefer_make_model         TEXT NOT NULL DEFAULT 'Carrier Transicold',
    num_zones                 INTEGER NOT NULL DEFAULT 1 CHECK (num_zones >= 1 AND num_zones <= 3),
    zone1_type                TEXT NOT NULL DEFAULT 'CHILLED' CHECK (zone1_type IN ('FROZEN', 'CHILLED', 'AMBIENT', 'MULTI_TEMP')),
    zone1_target_temp_c       REAL NOT NULL DEFAULT 4.0,
    zone1_min_temp_c          REAL NOT NULL DEFAULT 2.0,
    zone1_max_temp_c          REAL NOT NULL DEFAULT 8.0,
    zone2_type                TEXT CHECK (zone2_type IN ('FROZEN', 'CHILLED', 'AMBIENT') OR zone2_type IS NULL),
    zone2_target_temp_c       REAL,
    zone2_min_temp_c          REAL,
    zone2_max_temp_c          REAL,
    humidity_monitoring       BOOLEAN NOT NULL DEFAULT FALSE,
    target_humidity_rh        REAL,
    defrost_mode              TEXT NOT NULL DEFAULT 'AUTO' CHECK (defrost_mode IN ('AUTO', 'MANUAL', 'CYCLE')),
    created_at                TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_reefer_profiles_tenant
    ON vehicle_reefer_profiles(tenant_id);

CREATE TABLE IF NOT EXISTS vehicle_reefer_telemetry_latest (
    vehicle_id                TEXT PRIMARY KEY REFERENCES vehicles(id) ON DELETE CASCADE,
    tenant_id                 TEXT NOT NULL REFERENCES tenants(id),
    reefer_power_status       TEXT NOT NULL DEFAULT 'ON' CHECK (reefer_power_status IN ('OFF', 'ON', 'STANDBY', 'FAULT')),
    reefer_mode               TEXT NOT NULL DEFAULT 'COOLING' CHECK (reefer_mode IN ('COOLING', 'HEATING', 'DEFROST', 'IDLE', 'OFF')),
    zone1_temp_c              REAL NOT NULL,
    zone1_setpoint_c          REAL NOT NULL,
    zone2_temp_c              REAL,
    zone2_setpoint_c          REAL,
    humidity_rh               REAL,
    door_status               TEXT NOT NULL DEFAULT 'CLOSED' CHECK (door_status IN ('CLOSED', 'OPEN')),
    door_open_duration_sec    INTEGER NOT NULL DEFAULT 0,
    reefer_fuel_level_pct     REAL NOT NULL DEFAULT 100.0,
    reefer_battery_v          REAL NOT NULL DEFAULT 12.6,
    has_active_excursion      BOOLEAN NOT NULL DEFAULT FALSE,
    last_sync_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_reefer_telemetry_tenant_excursion
    ON vehicle_reefer_telemetry_latest(tenant_id, has_active_excursion);

CREATE TABLE IF NOT EXISTS reefer_temperature_excursions (
    id                        TEXT PRIMARY KEY,
    tenant_id                 TEXT NOT NULL REFERENCES tenants(id),
    vehicle_id                TEXT NOT NULL REFERENCES vehicles(id) ON DELETE CASCADE,
    trip_id                   TEXT REFERENCES trips(id) ON DELETE SET NULL,
    zone_number               INTEGER NOT NULL DEFAULT 1,
    excursion_type            TEXT NOT NULL CHECK (excursion_type IN ('OVER_TEMP', 'UNDER_TEMP', 'DOOR_PROLONGED_OPEN', 'SENSOR_FAULT')),
    severity                  TEXT NOT NULL DEFAULT 'WARNING' CHECK (severity IN ('WARNING', 'CRITICAL')),
    start_time                TIMESTAMPTZ NOT NULL,
    end_time                  TIMESTAMPTZ,
    duration_seconds          INTEGER NOT NULL DEFAULT 0,
    peak_temp_c               REAL NOT NULL,
    threshold_temp_c          REAL NOT NULL,
    setpoint_c                REAL NOT NULL,
    status                    TEXT NOT NULL DEFAULT 'OPEN' CHECK (status IN ('OPEN', 'ACKNOWLEDGED', 'RESOLVED')),
    resolution_note           TEXT NOT NULL DEFAULT '',
    resolved_by               TEXT NOT NULL DEFAULT '',
    created_at                TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_reefer_excursions_tenant_veh
    ON reefer_temperature_excursions(tenant_id, vehicle_id, status);

CREATE TABLE IF NOT EXISTS reefer_trip_summaries (
    id                        TEXT PRIMARY KEY,
    tenant_id                 TEXT NOT NULL REFERENCES tenants(id),
    trip_id                   TEXT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    vehicle_id                TEXT NOT NULL REFERENCES vehicles(id) ON DELETE CASCADE,
    start_time                TIMESTAMPTZ NOT NULL,
    end_time                  TIMESTAMPTZ NOT NULL,
    zone1_min_temp_c          REAL NOT NULL,
    zone1_max_temp_c          REAL NOT NULL,
    zone1_mean_kinetic_temp_c REAL NOT NULL,
    total_excursion_duration_sec INTEGER NOT NULL DEFAULT 0,
    door_open_count           INTEGER NOT NULL DEFAULT 0,
    compliance_status         TEXT NOT NULL DEFAULT 'COMPLIANT' CHECK (compliance_status IN ('COMPLIANT', 'BREACHED')),
    certified_at              TIMESTAMPTZ,
    certified_by              TEXT NOT NULL DEFAULT '',
    created_at                TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_reefer_trip_tenant_trip
    ON reefer_trip_summaries(tenant_id, trip_id);

-- RBAC Seeding & Grants
INSERT INTO permissions (name, description) VALUES
('coldchain:read', 'View reefer temperature telemetry, multi-zone probes, and excursion alerts'),
('coldchain:write', 'Configure reefer set-points, acknowledge excursions, and certify HACCP compliance')
ON CONFLICT (name) DO NOTHING;

-- Grant to Role 1 (Admin)
INSERT INTO role_permissions (role_id, permission_id)
SELECT 1, id FROM permissions WHERE name IN ('coldchain:read', 'coldchain:write')
ON CONFLICT DO NOTHING;

-- Grant to Role 2 (Dispatcher / Ops)
INSERT INTO role_permissions (role_id, permission_id)
SELECT 2, id FROM permissions WHERE name IN ('coldchain:read', 'coldchain:write')
ON CONFLICT DO NOTHING;

-- Grant to Role 3 (Maintenance)
INSERT INTO role_permissions (role_id, permission_id)
SELECT 3, id FROM permissions WHERE name IN ('coldchain:read', 'coldchain:write')
ON CONFLICT DO NOTHING;

-- Grant to Role 6 (Org Admin)
INSERT INTO role_permissions (role_id, permission_id)
SELECT 6, id FROM permissions WHERE name IN ('coldchain:read', 'coldchain:write')
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN (SELECT id FROM permissions WHERE name IN ('coldchain:read', 'coldchain:write'));
DELETE FROM permissions WHERE name IN ('coldchain:read', 'coldchain:write');

DROP INDEX IF EXISTS idx_reefer_trip_tenant_trip;
DROP TABLE IF EXISTS reefer_trip_summaries;

DROP INDEX IF EXISTS idx_reefer_excursions_tenant_veh;
DROP TABLE IF EXISTS reefer_temperature_excursions;

DROP INDEX IF EXISTS idx_reefer_telemetry_tenant_excursion;
DROP TABLE IF EXISTS vehicle_reefer_telemetry_latest;

DROP INDEX IF EXISTS idx_reefer_profiles_tenant;
DROP TABLE IF EXISTS vehicle_reefer_profiles;
