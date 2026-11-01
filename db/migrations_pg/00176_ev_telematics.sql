-- +goose Up
-- PG port of 00176_ev_telematics.sql
-- EV Telematics & Battery Management (FleetX Parity P3 3.10)
-- Real-time State of Charge (SoC), State of Health (SoH), dynamic range estimation,
-- automated charging session logging, and green energy analytics.

CREATE TABLE IF NOT EXISTS vehicle_ev_profiles (
    vehicle_id                TEXT PRIMARY KEY REFERENCES vehicles(id) ON DELETE CASCADE,
    tenant_id                 TEXT NOT NULL REFERENCES tenants(id),
    battery_capacity_kwh      REAL NOT NULL CHECK (battery_capacity_kwh > 0.0),
    max_range_km              REAL NOT NULL CHECK (max_range_km > 0.0),
    charging_standard         TEXT NOT NULL DEFAULT 'CCS2',
    min_safe_soc              REAL NOT NULL DEFAULT 15.0 CHECK (min_safe_soc >= 0.0 AND min_safe_soc <= 100.0),
    nominal_efficiency_wh_km  REAL NOT NULL DEFAULT 140.0 CHECK (nominal_efficiency_wh_km > 0.0),
    created_at                TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ev_profiles_tenant
    ON vehicle_ev_profiles(tenant_id);

CREATE TABLE IF NOT EXISTS vehicle_ev_telemetry_latest (
    vehicle_id                TEXT PRIMARY KEY REFERENCES vehicles(id) ON DELETE CASCADE,
    tenant_id                 TEXT NOT NULL REFERENCES tenants(id),
    soc_percent               REAL NOT NULL CHECK (soc_percent >= 0.0 AND soc_percent <= 100.0),
    soh_percent               REAL NOT NULL DEFAULT 100.0 CHECK (soh_percent >= 0.0 AND soh_percent <= 100.0),
    battery_temp_c            REAL,
    remaining_range_km        REAL NOT NULL DEFAULT 0.0,
    charging_status           TEXT NOT NULL DEFAULT 'DISCHARGING'
                              CHECK (charging_status IN ('DISCHARGING', 'CHARGING', 'FAST_CHARGING', 'COMPLETE', 'NOT_CHARGING')),
    charging_power_kw         REAL NOT NULL DEFAULT 0.0,
    current_draw_a            REAL NOT NULL DEFAULT 0.0,
    voltage_v                 REAL NOT NULL DEFAULT 0.0,
    last_sync_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ev_telemetry_tenant_status
    ON vehicle_ev_telemetry_latest(tenant_id, charging_status);

CREATE TABLE IF NOT EXISTS ev_charging_sessions (
    id                        TEXT PRIMARY KEY,
    tenant_id                 TEXT NOT NULL REFERENCES tenants(id),
    vehicle_id                TEXT NOT NULL REFERENCES vehicles(id) ON DELETE CASCADE,
    facility_id               TEXT REFERENCES facilities(id) ON DELETE SET NULL,
    start_time                TIMESTAMPTZ NOT NULL,
    end_time                  TIMESTAMPTZ,
    start_soc                 REAL NOT NULL,
    end_soc                   REAL,
    energy_added_kwh          REAL NOT NULL DEFAULT 0.0,
    peak_power_kw             REAL NOT NULL DEFAULT 0.0,
    charger_type              TEXT NOT NULL DEFAULT 'AC_SLOW' CHECK (charger_type IN ('AC_SLOW', 'DC_FAST')),
    estimated_cost            REAL NOT NULL DEFAULT 0.0,
    status                    TEXT NOT NULL DEFAULT 'IN_PROGRESS'
                              CHECK (status IN ('IN_PROGRESS', 'COMPLETED', 'INTERRUPTED')),
    created_at                TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ev_charging_tenant_veh
    ON ev_charging_sessions(tenant_id, vehicle_id, status);

CREATE TABLE IF NOT EXISTS ev_energy_summaries (
    id                        TEXT PRIMARY KEY,
    tenant_id                 TEXT NOT NULL REFERENCES tenants(id),
    vehicle_id                TEXT NOT NULL REFERENCES vehicles(id) ON DELETE CASCADE,
    trip_id                   TEXT REFERENCES trips(id) ON DELETE SET NULL,
    distance_km               REAL NOT NULL DEFAULT 0.0,
    energy_consumed_kwh       REAL NOT NULL DEFAULT 0.0,
    efficiency_wh_km          REAL NOT NULL DEFAULT 0.0,
    co2_avoided_kg            REAL NOT NULL DEFAULT 0.0,
    recorded_at               TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ev_energy_tenant_date
    ON ev_energy_summaries(tenant_id, recorded_at DESC);

-- RBAC Seeding & Grants
INSERT INTO permissions (name, description) VALUES
('ev:read', 'View EV fleet telemetry, battery status, charging sessions, and energy analytics'),
('ev:write', 'Configure EV specs, update battery profiles, and manage charging sessions')
ON CONFLICT (name) DO NOTHING;

-- Grant to Role 1 (Admin)
INSERT INTO role_permissions (role_id, permission_id)
SELECT 1, id FROM permissions WHERE name IN ('ev:read', 'ev:write')
ON CONFLICT DO NOTHING;

-- Grant to Role 2 (Dispatcher / Ops)
INSERT INTO role_permissions (role_id, permission_id)
SELECT 2, id FROM permissions WHERE name IN ('ev:read', 'ev:write')
ON CONFLICT DO NOTHING;

-- Grant to Role 3 (Maintenance)
INSERT INTO role_permissions (role_id, permission_id)
SELECT 3, id FROM permissions WHERE name IN ('ev:read', 'ev:write')
ON CONFLICT DO NOTHING;

-- Grant to Role 6 (Org Admin)
INSERT INTO role_permissions (role_id, permission_id)
SELECT 6, id FROM permissions WHERE name IN ('ev:read', 'ev:write')
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN (SELECT id FROM permissions WHERE name IN ('ev:read', 'ev:write'));
DELETE FROM permissions WHERE name IN ('ev:read', 'ev:write');

DROP INDEX IF EXISTS idx_ev_energy_tenant_date;
DROP TABLE IF EXISTS ev_energy_summaries;

DROP INDEX IF EXISTS idx_ev_charging_tenant_veh;
DROP TABLE IF EXISTS ev_charging_sessions;

DROP INDEX IF EXISTS idx_ev_telemetry_tenant_status;
DROP TABLE IF EXISTS vehicle_ev_telemetry_latest;

DROP INDEX IF EXISTS idx_ev_profiles_tenant;
DROP TABLE IF EXISTS vehicle_ev_profiles;
