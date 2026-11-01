-- PG port of 00170_telemetry_scale_indexes.sql | status: PORTABLE | flags: none
-- +goose Up
-- SQL in this section is executed when the migration is applied.

CREATE INDEX IF NOT EXISTS idx_positions_device_time ON telemetry_positions(device_time DESC);
CREATE INDEX IF NOT EXISTS idx_positions_trip_time ON telemetry_positions(trip_id, device_time DESC);
CREATE INDEX IF NOT EXISTS idx_raw_device_time ON telemetry_raw_events(device_time DESC);
CREATE INDEX IF NOT EXISTS idx_snapshots_timestamp ON telemetry_snapshots(timestamp DESC);
DROP INDEX IF EXISTS idx_telemetry_snapshots_vehicle_time;
CREATE INDEX IF NOT EXISTS idx_outbox_published_created_at ON outbox_events(created_at) WHERE published_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_vlp_trip ON vehicle_latest_position(trip_id);

INSERT INTO vehicle_latest_position (
    vehicle_id, tenant_id, device_time, latitude, longitude, speed, heading, ignition, fuel_level, odometer, trip_id, driver_id
)
SELECT
    s.vehicle_id,
    COALESCE(v.tenant_id, '1'),
    s.timestamp,
    s.latitude,
    s.longitude,
    s.speed,
    s.heading,
    s.ignition,
    s.fuel_level,
    s.odometer,
    s.trip_id,
    s.driver_id
FROM telemetry_snapshots s
JOIN vehicles v ON v.id = s.vehicle_id
JOIN (
    SELECT vehicle_id, MAX(timestamp) AS max_ts
    FROM telemetry_snapshots
    WHERE vehicle_id IS NOT NULL AND latitude IS NOT NULL AND longitude IS NOT NULL
    GROUP BY vehicle_id
) latest ON latest.vehicle_id = s.vehicle_id AND latest.max_ts = s.timestamp
GROUP BY s.vehicle_id, v.tenant_id, s.timestamp, s.latitude, s.longitude, s.speed, s.heading, s.ignition, s.fuel_level, s.odometer, s.trip_id, s.driver_id
ON CONFLICT (vehicle_id) DO NOTHING;

-- +goose Down
DROP INDEX IF EXISTS idx_vlp_trip;
DROP INDEX IF EXISTS idx_outbox_published_created_at;
CREATE INDEX IF NOT EXISTS idx_telemetry_snapshots_vehicle_time ON telemetry_snapshots(vehicle_id, timestamp);
DROP INDEX IF EXISTS idx_snapshots_timestamp;
DROP INDEX IF EXISTS idx_raw_device_time;
DROP INDEX IF EXISTS idx_positions_trip_time;
DROP INDEX IF EXISTS idx_positions_device_time;
