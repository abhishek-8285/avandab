-- PG port of 00008_invoices.sql | status: PORTABLE | flags: none
-- +goose Up
CREATE TABLE invoices (
    id             TEXT PRIMARY KEY,
    invoice_number TEXT NOT NULL UNIQUE,
    booking_id     TEXT NOT NULL,
    customer_id    TEXT NOT NULL,
    trip_id        TEXT,
    subtotal       REAL NOT NULL,
    tax            REAL NOT NULL DEFAULT 0.0,
    discount       REAL NOT NULL DEFAULT 0.0,
    total          REAL NOT NULL,
    payment_status TEXT NOT NULL DEFAULT 'pending' CHECK (payment_status IN ('pending', 'paid', 'partially_paid')),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT (CURRENT_TIMESTAMP),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT (CURRENT_TIMESTAMP),
    FOREIGN KEY (booking_id) REFERENCES bookings(id),
    FOREIGN KEY (customer_id) REFERENCES customers(id),
    FOREIGN KEY (trip_id) REFERENCES trips(id)
);
-- +goose Down
DROP TABLE IF EXISTS invoices;
