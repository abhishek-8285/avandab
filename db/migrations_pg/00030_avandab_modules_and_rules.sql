-- PG port of 00030_avandab_modules_and_rules.sql | status: PORTABLE | flags: none
-- +goose Up
-- Add compliance, POD, telemetry alert and financial settlement columns and tables for Avandab Architecture.

ALTER TABLE drivers ADD COLUMN blocked INTEGER NOT NULL DEFAULT 0;
ALTER TABLE drivers ADD COLUMN blocked_reason TEXT;
ALTER TABLE drivers ADD COLUMN aadhaar TEXT;
ALTER TABLE drivers ADD COLUMN pan TEXT;
ALTER TABLE drivers ADD COLUMN bank_details TEXT;
ALTER TABLE vehicles ADD COLUMN blocked INTEGER NOT NULL DEFAULT 0;
ALTER TABLE vehicles ADD COLUMN blocked_reason TEXT;
ALTER TABLE vehicles ADD COLUMN rc_expiry DATE;
ALTER TABLE vehicles ADD COLUMN odometer REAL NOT NULL DEFAULT 0.0;
ALTER TABLE trips ADD COLUMN eway_bill_ref TEXT;
ALTER TABLE trips ADD COLUMN pod_url TEXT;
ALTER TABLE trips ADD COLUMN final_settlement_amount REAL NOT NULL DEFAULT 0.0;
CREATE TABLE IF NOT EXISTS compliance_checks (
    id TEXT PRIMARY KEY,
    entity_type TEXT NOT NULL CHECK (entity_type IN ('driver', 'vehicle', 'cargo')),
    entity_id TEXT NOT NULL,
    check_type TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('valid', 'expired', 'blocked', 'warning')),
    details TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS telemetry_alerts (
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
CREATE TABLE IF NOT EXISTS driver_settlements (
    id TEXT PRIMARY KEY,
    trip_id TEXT NOT NULL UNIQUE,
    driver_id TEXT NOT NULL,
    gross_fare REAL NOT NULL DEFAULT 0.0,
    advances_kharcha REAL NOT NULL DEFAULT 0.0,
    deductions REAL NOT NULL DEFAULT 0.0,
    net_payout REAL NOT NULL DEFAULT 0.0,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'paid', 'disputed')),
    payment_ref TEXT,
    paid_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (trip_id) REFERENCES trips(id),
    FOREIGN KEY (driver_id) REFERENCES drivers(id)
);
CREATE INDEX IF NOT EXISTS idx_telemetry_alerts_trip ON telemetry_alerts(trip_id);
CREATE INDEX IF NOT EXISTS idx_driver_settlements_trip ON driver_settlements(trip_id);
-- +goose Down
DROP TABLE IF EXISTS driver_settlements;
DROP TABLE IF EXISTS telemetry_alerts;
DROP TABLE IF EXISTS compliance_checks;
