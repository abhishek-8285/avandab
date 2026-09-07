-- PG port of 00006_bookings.sql | status: PORTABLE | flags: none
-- +goose Up
CREATE TABLE bookings (
    id             TEXT PRIMARY KEY,
    booking_number TEXT NOT NULL UNIQUE,
    customer_id    TEXT NOT NULL,
    pickup_date    TIMESTAMPTZ NOT NULL,
    route_id       TEXT NOT NULL,
    vehicle_type   TEXT NOT NULL,
    passengers     INTEGER NOT NULL DEFAULT 1,
    cargo_weight   REAL,
    price          REAL NOT NULL,
    notes          TEXT,
    status         TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'confirmed', 'cancelled', 'completed')),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT (CURRENT_TIMESTAMP),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT (CURRENT_TIMESTAMP),
    FOREIGN KEY (customer_id) REFERENCES customers(id),
    FOREIGN KEY (route_id) REFERENCES routes(id)
);
-- +goose Down
DROP TABLE IF EXISTS bookings;
