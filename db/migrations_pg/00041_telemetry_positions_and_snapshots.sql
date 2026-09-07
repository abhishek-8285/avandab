-- PG port of 00041_telemetry_positions_and_snapshots.sql | status: PORTABLE | flags: none
-- +goose Up
-- Position history (per accepted frame, after raw-log + guards)
CREATE TABLE telemetry_positions (
    id           TEXT PRIMARY KEY,
    tenant_id    TEXT NOT NULL DEFAULT '1',
    imei         TEXT NOT NULL,
    device_time  TIMESTAMPTZ NOT NULL,
    received_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    latitude     REAL NOT NULL,
    longitude    REAL NOT NULL,
    speed        REAL,
    heading      REAL,
    ignition     INTEGER,
    engine_hours REAL,
    accuracy     REAL,
    fuel_level   REAL,
    odometer     REAL,
    driver_id    TEXT,
    trip_id      TEXT,
    vehicle_id   TEXT,
    provider     TEXT NOT NULL DEFAULT 'own',
    raw_event_id TEXT,
    FOREIGN KEY (raw_event_id) REFERENCES telemetry_raw_events(id),
    FOREIGN KEY (vehicle_id)   REFERENCES vehicles(id),
    FOREIGN KEY (trip_id)      REFERENCES trips(id),
    FOREIGN KEY (driver_id)    REFERENCES drivers(id)
);
CREATE INDEX idx_positions_imei_time ON telemetry_positions(imei, device_time DESC);
CREATE INDEX idx_positions_vehicle_time ON telemetry_positions(vehicle_id, device_time DESC);
CREATE INDEX idx_positions_raw_event ON telemetry_positions(raw_event_id);
-- Latest position per vehicle (upsert; only newer device_time wins)
CREATE TABLE vehicle_latest_position (
    vehicle_id   TEXT PRIMARY KEY,
    tenant_id    TEXT NOT NULL DEFAULT '1',
    imei         TEXT,
    device_time  TIMESTAMPTZ NOT NULL,
    received_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    latitude     REAL NOT NULL,
    longitude    REAL NOT NULL,
    speed        REAL,
    heading      REAL,
    ignition     INTEGER,
    engine_hours REAL,
    accuracy     REAL,
    fuel_level   REAL,
    odometer     REAL,
    driver_id    TEXT,
    trip_id      TEXT,
    FOREIGN KEY (vehicle_id) REFERENCES vehicles(id)
);
-- Snapshot enrichment (existing table from 00031; consumers rely on these fields)
-- NOTE (Decision D5): snapshot IDs were historically 'snap-<timestamp>' and will
-- now be UUIDs. Old rows keep their 'snap-*' IDs; consumers must handle both.
ALTER TABLE telemetry_snapshots ADD COLUMN heading REAL;
ALTER TABLE telemetry_snapshots ADD COLUMN ignition INTEGER;
ALTER TABLE telemetry_snapshots ADD COLUMN engine_hours REAL;
ALTER TABLE telemetry_snapshots ADD COLUMN accuracy REAL;
ALTER TABLE telemetry_snapshots ADD COLUMN driver_id TEXT;
-- +goose Down
DROP TABLE IF EXISTS vehicle_latest_position;
DROP TABLE IF EXISTS telemetry_positions;
ALTER TABLE telemetry_snapshots DROP COLUMN heading;
ALTER TABLE telemetry_snapshots DROP COLUMN ignition;
ALTER TABLE telemetry_snapshots DROP COLUMN engine_hours;
ALTER TABLE telemetry_snapshots DROP COLUMN accuracy;
ALTER TABLE telemetry_snapshots DROP COLUMN driver_id;
