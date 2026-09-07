-- PG port of 00016_add_tenant_id_to_bookings.sql | status: PORTABLE | flags: none
-- +goose Up
ALTER TABLE bookings ADD COLUMN tenant_id TEXT DEFAULT '1' NOT NULL;
-- +goose Down
-- Sqlite doesn't support easily dropping columns in older versions, but we can standard rollback.
