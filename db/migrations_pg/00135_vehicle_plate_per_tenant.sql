-- PG port of 00135_vehicle_plate_per_tenant.sql
-- +goose Up
-- 00135 — plates are per-tenant, not global (H3) + latest-position tenant
-- uniqueness (L10). Native ALTERs (no rebuild needed on Postgres).
ALTER TABLE vehicles DROP CONSTRAINT IF EXISTS vehicles_registration_number_key;
ALTER TABLE vehicles ADD CONSTRAINT vehicles_tenant_plate_unique UNIQUE (tenant_id, registration_number);
CREATE UNIQUE INDEX IF NOT EXISTS idx_vehicle_latest_position_tenant_vehicle
    ON vehicle_latest_position(tenant_id, vehicle_id);
-- +goose Down
DROP INDEX IF EXISTS idx_vehicle_latest_position_tenant_vehicle;
ALTER TABLE vehicles DROP CONSTRAINT IF EXISTS vehicles_tenant_plate_unique;
ALTER TABLE vehicles ADD CONSTRAINT vehicles_registration_number_key UNIQUE (registration_number);
