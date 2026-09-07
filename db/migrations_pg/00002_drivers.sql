-- PG port of 00002_drivers.sql | status: PORTABLE | flags: none
-- +goose Up
CREATE TABLE drivers (
    id                  TEXT PRIMARY KEY,
    driver_id           TEXT NOT NULL UNIQUE,
    first_name          TEXT NOT NULL,
    last_name           TEXT NOT NULL,
    phone               TEXT NOT NULL,
    email               TEXT,
    address             TEXT,
    license_number      TEXT NOT NULL,
    license_expiry      DATE NOT NULL,
    experience_years    INTEGER NOT NULL DEFAULT 0,
    status              TEXT NOT NULL DEFAULT 'available' CHECK (status IN ('available', 'on_trip', 'leave', 'inactive')),
    emergency_contact_name TEXT,
    emergency_contact_phone TEXT,
    notes               TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT (CURRENT_TIMESTAMP),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT (CURRENT_TIMESTAMP)
);
-- +goose Down
DROP TABLE IF EXISTS drivers;
