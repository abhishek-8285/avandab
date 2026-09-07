-- PG port of 00017_add_tenant_id_to_trips.sql | status: PORTABLE | flags: none
-- +goose Up
ALTER TABLE trips ADD COLUMN tenant_id TEXT DEFAULT '1' NOT NULL;
-- +goose Down
