-- PG port of 00032_kharcha_and_epod.sql | status: PORTABLE | flags: none
-- +goose Up
-- Kharcha Ledger: Add workflow columns to driver_expenses, add e-POD fields to trips.

-- Kharcha workflow columns on driver_expenses
ALTER TABLE driver_expenses ADD COLUMN status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected', 'settled'));
ALTER TABLE driver_expenses ADD COLUMN category TEXT NOT NULL DEFAULT 'advance' CHECK (category IN ('advance', 'fuel', 'toll', 'food', 'repair', 'other'));
ALTER TABLE driver_expenses ADD COLUMN requested_by TEXT REFERENCES drivers(id);
ALTER TABLE driver_expenses ADD COLUMN approved_by TEXT REFERENCES users(id);
ALTER TABLE driver_expenses ADD COLUMN rejected_reason TEXT;
ALTER TABLE driver_expenses ADD COLUMN approved_at TIMESTAMPTZ;
-- e-POD enrichment columns on trips
ALTER TABLE trips ADD COLUMN pod_photo_url TEXT;
ALTER TABLE trips ADD COLUMN pod_signature_url TEXT;
ALTER TABLE trips ADD COLUMN pod_consignee_name TEXT;
ALTER TABLE trips ADD COLUMN pod_consignee_phone TEXT;
ALTER TABLE trips ADD COLUMN pod_otp_verified INTEGER NOT NULL DEFAULT 0;
ALTER TABLE trips ADD COLUMN pod_captured_at TIMESTAMPTZ;
ALTER TABLE trips ADD COLUMN pod_lat REAL;
ALTER TABLE trips ADD COLUMN pod_lng REAL;
ALTER TABLE trips ADD COLUMN pod_notes TEXT;
-- Performance indexes
CREATE INDEX IF NOT EXISTS idx_driver_expenses_status ON driver_expenses(status);
CREATE INDEX IF NOT EXISTS idx_driver_expenses_trip ON driver_expenses(trip_id);
CREATE INDEX IF NOT EXISTS idx_driver_expenses_driver ON driver_expenses(driver_id);
-- +goose Down
DROP INDEX IF EXISTS idx_driver_expenses_driver;
DROP INDEX IF EXISTS idx_driver_expenses_trip;
DROP INDEX IF EXISTS idx_driver_expenses_status;
