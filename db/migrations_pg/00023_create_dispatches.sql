-- PG port of 00023_create_dispatches.sql | status: PORTABLE | flags: none
-- +goose Up
CREATE TABLE IF NOT EXISTS dispatches (
    id TEXT PRIMARY KEY,
    dispatch_no TEXT UNIQUE NOT NULL,
    dispatcher_id TEXT NOT NULL,
    booking_id TEXT NOT NULL,
    driver_id TEXT,
    vehicle_id TEXT,
    scheduled_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL DEFAULT 'draft',
    trip_id TEXT,
    notes TEXT,
    tenant_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (dispatcher_id) REFERENCES users(id),
    FOREIGN KEY (booking_id) REFERENCES bookings(id),
    FOREIGN KEY (driver_id) REFERENCES drivers(id),
    FOREIGN KEY (vehicle_id) REFERENCES vehicles(id),
    FOREIGN KEY (trip_id) REFERENCES trips(id)
);
CREATE INDEX IF NOT EXISTS idx_dispatches_status ON dispatches(status);
CREATE INDEX IF NOT EXISTS idx_dispatches_booking_id ON dispatches(booking_id);
-- +goose Down
DROP TABLE IF EXISTS dispatches;
