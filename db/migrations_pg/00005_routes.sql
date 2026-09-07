-- PG port of 00005_routes.sql | status: PORTABLE | flags: none
-- +goose Up
CREATE TABLE routes (
    id              TEXT PRIMARY KEY,
    source          TEXT NOT NULL,
    destination     TEXT NOT NULL,
    distance        REAL NOT NULL,
    estimated_hours REAL NOT NULL,
    standard_fare   REAL NOT NULL,
    remarks         TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT (CURRENT_TIMESTAMP),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT (CURRENT_TIMESTAMP)
);
-- +goose Down
DROP TABLE IF EXISTS routes;
