-- PG port of 00004_customers.sql | status: PORTABLE | flags: none
-- +goose Up
CREATE TABLE customers (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    company     TEXT,
    phone       TEXT NOT NULL,
    email       TEXT,
    gst         TEXT,
    address     TEXT,
    notes       TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT (CURRENT_TIMESTAMP),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT (CURRENT_TIMESTAMP)
);
-- +goose Down
DROP TABLE IF EXISTS customers;
