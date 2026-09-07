-- PG port of 00059_telemetry_alerts_rebuild.sql | status: NEEDS-REVIEW | flags: STRIPPED-PRAGMA | reviewed: YES
-- +goose Up
CREATE TABLE telemetry_alerts_new (
    id TEXT PRIMARY KEY,
    trip_id TEXT,
    vehicle_id TEXT,
    driver_id TEXT,
    alert_type TEXT NOT NULL CHECK (alert_type IN
        ('night_driving','restricted_zone','unauthorized_movement','off_hours_use',
         'refill','theft_suspicion','abnormal_drain','siphon_confirmed','odometer_rollback',
         'speeding','temp_breach','gps_deviation','geofence_breach')),
    severity TEXT NOT NULL DEFAULT 'warning',
    details TEXT NOT NULL,
    latitude REAL,
    longitude REAL,
    resolved INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (trip_id) REFERENCES trips(id),
    FOREIGN KEY (vehicle_id) REFERENCES vehicles(id),
    FOREIGN KEY (driver_id) REFERENCES drivers(id)
);
INSERT INTO telemetry_alerts_new (id, trip_id, vehicle_id, driver_id, alert_type, severity, details, latitude, longitude, resolved, created_at)
    SELECT id, trip_id, vehicle_id, driver_id,
           CASE alert_type WHEN 'fuel_theft' THEN 'theft_suspicion' ELSE alert_type END,
           severity, details, latitude, longitude, resolved, created_at
    FROM telemetry_alerts;
DROP TABLE telemetry_alerts;
ALTER TABLE telemetry_alerts_new RENAME TO telemetry_alerts;
CREATE INDEX IF NOT EXISTS idx_telemetry_alerts_trip ON telemetry_alerts(trip_id);
CREATE INDEX IF NOT EXISTS idx_telemetry_alerts_type ON telemetry_alerts(alert_type, created_at);
-- +goose Down
CREATE TABLE telemetry_alerts_old (
    id TEXT PRIMARY KEY,
    trip_id TEXT,
    vehicle_id TEXT,
    driver_id TEXT,
    alert_type TEXT NOT NULL CHECK (alert_type IN ('gps_deviation', 'fuel_theft', 'temp_breach', 'speeding')),
    severity TEXT NOT NULL DEFAULT 'warning',
    details TEXT NOT NULL,
    latitude REAL,
    longitude REAL,
    resolved INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (trip_id) REFERENCES trips(id),
    FOREIGN KEY (vehicle_id) REFERENCES vehicles(id),
    FOREIGN KEY (driver_id) REFERENCES drivers(id)
);
INSERT INTO telemetry_alerts_old (id, trip_id, vehicle_id, driver_id, alert_type, severity, details, latitude, longitude, resolved, created_at)
    SELECT id, trip_id, vehicle_id, driver_id,
           CASE alert_type WHEN 'theft_suspicion' THEN 'fuel_theft' ELSE alert_type END,
           severity, details, latitude, longitude, resolved, created_at
    FROM telemetry_alerts
    WHERE alert_type IN ('gps_deviation','temp_breach','speeding','theft_suspicion','fuel_theft');
DROP TABLE telemetry_alerts;
ALTER TABLE telemetry_alerts_old RENAME TO telemetry_alerts;
CREATE INDEX IF NOT EXISTS idx_telemetry_alerts_trip ON telemetry_alerts(trip_id);
