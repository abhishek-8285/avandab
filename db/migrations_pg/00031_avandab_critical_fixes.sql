-- PG port of 00031_avandab_critical_fixes.sql | status: PORTABLE | flags: none
-- +goose Up
-- Add critical schema fixes: customers, bidirectional routes, telemetry_snapshots, eway_bills, driver_expenses.

CREATE TABLE IF NOT EXISTS telemetry_snapshots (
  id TEXT PRIMARY KEY,
  trip_id TEXT,
  vehicle_id TEXT,
  timestamp TIMESTAMPTZ NOT NULL,
  latitude REAL,
  longitude REAL,
  speed REAL,
  fuel_level REAL,
  odometer REAL,
  FOREIGN KEY (trip_id) REFERENCES trips(id),
  FOREIGN KEY (vehicle_id) REFERENCES vehicles(id)
);
CREATE INDEX IF NOT EXISTS idx_telemetry_snapshots_trip ON telemetry_snapshots(trip_id, timestamp);
CREATE TABLE IF NOT EXISTS eway_bills (
  id TEXT PRIMARY KEY,
  trip_id TEXT UNIQUE,
  ewb_number TEXT UNIQUE NOT NULL,
  irn TEXT UNIQUE,
  generation_date TIMESTAMPTZ NOT NULL,
  valid_until TIMESTAMPTZ NOT NULL,
  transporter_id TEXT,
  vehicle_number TEXT,
  status TEXT DEFAULT 'active' CHECK (status IN ('active', 'cancelled', 'expired')),
  raw_response TEXT,
  created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (trip_id) REFERENCES trips(id)
);
CREATE TABLE IF NOT EXISTS driver_expenses (
  id TEXT PRIMARY KEY,
  trip_id TEXT,
  driver_id TEXT,
  expense_type TEXT NOT NULL CHECK (expense_type IN ('fuel', 'toll', 'food', 'repair', 'advance')),
  amount REAL NOT NULL,
  description TEXT,
  receipt_url TEXT,
  approved INTEGER DEFAULT 0,
  created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (trip_id) REFERENCES trips(id),
  FOREIGN KEY (driver_id) REFERENCES drivers(id)
);
ALTER TABLE routes ADD COLUMN reverse_distance REAL;
ALTER TABLE routes ADD COLUMN reverse_standard_fare REAL;
-- +goose Down
DROP TABLE IF EXISTS driver_expenses;
DROP TABLE IF EXISTS eway_bills;
DROP TABLE IF EXISTS telemetry_snapshots;
