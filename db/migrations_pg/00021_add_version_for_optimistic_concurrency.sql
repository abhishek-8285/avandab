-- PG port of 00021_add_version_for_optimistic_concurrency.sql | status: PORTABLE | flags: none
-- +goose Up
ALTER TABLE bookings ADD COLUMN version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE trips ADD COLUMN version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE drivers ADD COLUMN version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE vehicles ADD COLUMN version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE invoices ADD COLUMN version INTEGER NOT NULL DEFAULT 1;
-- +goose Down
-- SQLite does not support dropping columns easily, we default to standard rollback or no-op since it's a non-destructive column addition.
