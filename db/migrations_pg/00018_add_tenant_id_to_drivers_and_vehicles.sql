-- PG port of 00018_add_tenant_id_to_drivers_and_vehicles.sql | status: PORTABLE | flags: none
-- +goose Up
ALTER TABLE drivers ADD COLUMN tenant_id TEXT DEFAULT '1' NOT NULL;
ALTER TABLE vehicles ADD COLUMN tenant_id TEXT DEFAULT '1' NOT NULL;
-- +goose Down
