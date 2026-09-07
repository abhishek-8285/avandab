-- PG port of 00042_geofence_engine.sql | status: NEEDS-REVIEW | flags: HAS-DQUOTE | reviewed: YES
-- +goose Up
-- 1. Geofence definitions
CREATE TABLE geofences (
    id          TEXT PRIMARY KEY,
    tenant_id   TEXT NOT NULL DEFAULT '1',
    name        TEXT NOT NULL,
    kind        TEXT NOT NULL CHECK (kind IN ('pickup','drop','depot','restricted','no_entry')),
    shape       TEXT NOT NULL CHECK (shape IN ('circle','polygon')),
    center_lat  REAL,
    center_lng  REAL,
    radius_m    REAL,
    polygon     TEXT,                          -- JSON [[lat,lng],...] (closed ring)
    route_name  TEXT,                          -- fallback: matches routes.source/destination
    priority    INTEGER NOT NULL DEFAULT 0,
    is_active   INTEGER NOT NULL DEFAULT 1,
    created_by  TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_geofences_tenant_active ON geofences(tenant_id, is_active);
-- 2. Explicit per-vehicle bindings (for depot/restricted/no_entry)
CREATE TABLE vehicle_geofences (
    vehicle_id   TEXT NOT NULL,
    geofence_id  TEXT NOT NULL,
    tenant_id    TEXT NOT NULL DEFAULT '1',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (vehicle_id, geofence_id),
    FOREIGN KEY (vehicle_id) REFERENCES vehicles(id),
    FOREIGN KEY (geofence_id) REFERENCES geofences(id)
);
-- 3. Durable event + alert log
CREATE TABLE geofence_events (
    id          TEXT PRIMARY KEY,
    tenant_id   TEXT NOT NULL DEFAULT '1',
    vehicle_id  TEXT,
    trip_id     TEXT,
    geofence_id TEXT,
    zone_kind   TEXT,
    event_type  TEXT NOT NULL CHECK (event_type IN ('entering','inside','leaving','outside','breach','alert')),
    alert_type  TEXT,
    severity    TEXT,
    latitude    REAL,
    longitude   REAL,
    details     TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_geofence_events_vehicle ON geofence_events(vehicle_id, created_at);
-- 4. Per-vehicle dwell state machine (survives restarts)
CREATE TABLE engine_state (
    vehicle_id          TEXT PRIMARY KEY,
    tenant_id           TEXT NOT NULL DEFAULT '1',
    state               TEXT NOT NULL DEFAULT 'outside' CHECK (state IN ('outside','entering','inside','leaving')),
    trip_id             TEXT,
    geofence_id         TEXT,
    zone_kind           TEXT,
    zone_entered_at     TIMESTAMPTZ,
    confirmed_at        TIMESTAMPTZ,
    exit_started_at     TIMESTAMPTZ,
    last_fix_at         TIMESTAMPTZ,
    last_lat            REAL,
    last_lng            REAL,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (vehicle_id) REFERENCES vehicles(id)
);
-- 5. Detention tracking (pickup/drop dwell windows)
CREATE TABLE trip_detentions (
    id               TEXT PRIMARY KEY,
    tenant_id        TEXT NOT NULL DEFAULT '1',
    trip_id          TEXT NOT NULL,
    vehicle_id       TEXT,
    geofence_id      TEXT,
    zone_kind        TEXT NOT NULL CHECK (zone_kind IN ('pickup','drop')),
    entered_at       TIMESTAMPTZ NOT NULL,
    exited_at        TIMESTAMPTZ,
    dwell_seconds    INTEGER NOT NULL DEFAULT 0,
    free_seconds     INTEGER NOT NULL DEFAULT 0,
    billable_seconds INTEGER NOT NULL DEFAULT 0,
    rate_per_hour    REAL NOT NULL DEFAULT 0,
    amount           REAL NOT NULL DEFAULT 0,
    status           TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','closed','attached','invoiced','waived')),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (trip_id) REFERENCES trips(id),
    FOREIGN KEY (vehicle_id) REFERENCES vehicles(id),
    FOREIGN KEY (geofence_id) REFERENCES geofences(id)
);
CREATE INDEX idx_trip_detentions_trip ON trip_detentions(trip_id);
-- 6. Invoice line items (for detention billing, populated in 1G)
CREATE TABLE invoice_line_items (
    id          TEXT PRIMARY KEY,
    tenant_id   TEXT NOT NULL DEFAULT '1',
    invoice_id  TEXT NOT NULL,
    trip_id     TEXT,
    line_type   TEXT NOT NULL CHECK (line_type IN ('freight','detention','accessorial')),
    description TEXT NOT NULL,
    quantity    REAL NOT NULL DEFAULT 1,
    unit_price  REAL NOT NULL DEFAULT 0,
    amount      REAL NOT NULL DEFAULT 0,
    ref_id      TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (invoice_id) REFERENCES invoices(id),
    FOREIGN KEY (trip_id) REFERENCES trips(id)
);
-- 7. Vehicles additions (needed for fuel spec 03 and maintenance 04)
ALTER TABLE vehicles ADD COLUMN tank_capacity_litres REAL;
ALTER TABLE vehicles ADD COLUMN fuel_sensor_fitted INTEGER NOT NULL DEFAULT 0;
ALTER TABLE vehicles ADD COLUMN maintenance_due DATE;
-- 8. Canonical company_config table
CREATE TABLE company_config (
    tenant_id  TEXT NOT NULL DEFAULT '1',
    key        TEXT NOT NULL,
    value      TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (tenant_id, key)
);
-- 9. RBAC seeds
INSERT INTO permissions (name, description) VALUES
('geofences:create', 'Create geofences'),
('geofences:read', 'Read geofences'),
('geofences:update', 'Update geofences'),
('geofences:delete', 'Delete geofences') ON CONFLICT DO NOTHING;
INSERT INTO role_permissions (role_id, permission_id)
SELECT 1, id FROM permissions WHERE name LIKE 'geofences:%' ON CONFLICT DO NOTHING;
INSERT INTO role_permissions (role_id, permission_id)
SELECT 2, id FROM permissions WHERE name LIKE 'geofences:%' ON CONFLICT DO NOTHING;
-- 10. ADDITION (not in spec DDL): polling index for the dwell worker.
-- The worker queries "snapshots newer than engine_state.last_fix_at" per
-- vehicle each tick; (vehicle_id, timestamp) is the hot path.
CREATE INDEX idx_telemetry_snapshots_vehicle_timestamp ON telemetry_snapshots(vehicle_id, timestamp);
-- +goose Down
DROP TABLE IF EXISTS invoice_line_items;
DROP TABLE IF EXISTS trip_detentions;
DROP TABLE IF EXISTS engine_state;
DROP TABLE IF EXISTS geofence_events;
DROP TABLE IF EXISTS vehicle_geofences;
DROP TABLE IF EXISTS geofences;
DROP TABLE IF EXISTS company_config;
ALTER TABLE vehicles DROP COLUMN tank_capacity_litres;
ALTER TABLE vehicles DROP COLUMN fuel_sensor_fitted;
ALTER TABLE vehicles DROP COLUMN maintenance_due;
DELETE FROM permissions WHERE name LIKE 'geofences:%';
DROP INDEX IF EXISTS idx_telemetry_snapshots_vehicle_timestamp;
