-- PG port of 00007_trips.sql | status: PORTABLE | flags: none
-- +goose Up
CREATE TABLE trips (
    id            TEXT PRIMARY KEY,
    trip_number   TEXT NOT NULL UNIQUE,
    booking_id    TEXT,
    driver_id     TEXT,
    vehicle_id    TEXT,
    route_id      TEXT NOT NULL,
    departure_time TIMESTAMPTZ NOT NULL,
    arrival_time   TIMESTAMPTZ,
    status        TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'scheduled', 'assigned', 'started', 'completed', 'cancelled')),
    remarks       TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT (CURRENT_TIMESTAMP),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT (CURRENT_TIMESTAMP),
    FOREIGN KEY (booking_id) REFERENCES bookings(id),
    FOREIGN KEY (driver_id) REFERENCES drivers(id),
    FOREIGN KEY (vehicle_id) REFERENCES vehicles(id),
    FOREIGN KEY (route_id) REFERENCES routes(id)
);
-- +goose Down
DROP TABLE IF EXISTS trips;
