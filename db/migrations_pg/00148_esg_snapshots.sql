-- PG port of 00148_esg_snapshots.sql | status: PORTABLE | flags: none
-- +goose Up

CREATE TABLE IF NOT EXISTS trip_esg_metrics (
    id                   TEXT PRIMARY KEY,
    tenant_id            TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    trip_id              TEXT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    distance_km          DOUBLE PRECISION NOT NULL CHECK (distance_km >= 0),
    payload_tonnes       DOUBLE PRECISION NOT NULL CHECK (payload_tonnes >= 0),
    fuel_consumed_litres DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (fuel_consumed_litres >= 0),
    co2e_kg              DOUBLE PRECISION NOT NULL CHECK (co2e_kg >= 0),
    co2e_per_tkm         DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (co2e_per_tkm >= 0),
    emission_norm        TEXT NOT NULL CHECK (emission_norm IN ('BS3', 'BS4', 'BS6', 'EV', 'CNG', 'OTHER')),
    methodology          TEXT NOT NULL DEFAULT 'FUEL_PRIMARY' CHECK (methodology IN ('FUEL_PRIMARY', 'DISTANCE_ACTIVITY', 'DEFAULT_FACTOR')),
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, trip_id)
);

CREATE INDEX IF NOT EXISTS idx_trip_esg_tenant
    ON trip_esg_metrics(tenant_id, created_at);

CREATE TABLE IF NOT EXISTS esg_emission_snapshots (
    id                TEXT PRIMARY KEY,
    tenant_id         TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    period_start      TEXT NOT NULL,
    period_end        TEXT NOT NULL,
    total_trips       INTEGER NOT NULL CHECK (total_trips >= 0),
    total_distance_km DOUBLE PRECISION NOT NULL CHECK (total_distance_km >= 0),
    total_cargo_tkm   DOUBLE PRECISION NOT NULL CHECK (total_cargo_tkm >= 0),
    total_fuel_litres DOUBLE PRECISION NOT NULL CHECK (total_fuel_litres >= 0),
    total_co2e_kg     DOUBLE PRECISION NOT NULL CHECK (total_co2e_kg >= 0),
    avg_co2e_per_tkm  DOUBLE PRECISION NOT NULL CHECK (avg_co2e_per_tkm >= 0),
    ev_distance_km    DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (ev_distance_km >= 0),
    bs6_distance_km   DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (bs6_distance_km >= 0),
    bs4_distance_km   DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (bs4_distance_km >= 0),
    created_by        TEXT NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, period_start, period_end)
);

CREATE INDEX IF NOT EXISTS idx_esg_snapshots_period
    ON esg_emission_snapshots(tenant_id, period_start, period_end);

-- RBAC permissions: esg:read, esg:write
INSERT INTO permissions (name, description) VALUES
    ('esg:read', 'View ESG carbon emission metrics and snapshots'),
    ('esg:write', 'Generate and calculate ESG emission snapshots')
ON CONFLICT (name) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT 1, id FROM permissions WHERE name IN ('esg:read', 'esg:write')
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT 2, id FROM permissions WHERE name IN ('esg:read', 'esg:write')
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN (
    SELECT id FROM permissions WHERE name IN ('esg:read', 'esg:write')
);
DELETE FROM permissions WHERE name IN ('esg:read', 'esg:write');
DROP INDEX IF EXISTS idx_esg_snapshots_period;
DROP TABLE IF EXISTS esg_emission_snapshots;
DROP INDEX IF EXISTS idx_trip_esg_tenant;
DROP TABLE IF EXISTS trip_esg_metrics;
