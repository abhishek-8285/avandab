-- PG port of 00132_vehicle_expiry_nullable.sql
-- +goose Up
-- 00132 — vehicles.insurance_expiry / fitness_expiry / permit_expiry go
-- nullable (unknown doc date is NULL, never an invented +1y placeholder).
-- Native ALTER (no rebuild needed on Postgres).
-- NO BACKFILL (same as sqlite): fabricated +1y dates are indistinguishable
-- from real ones; only new rows are honest.
ALTER TABLE vehicles ALTER COLUMN insurance_expiry DROP NOT NULL;
ALTER TABLE vehicles ALTER COLUMN fitness_expiry DROP NOT NULL;
ALTER TABLE vehicles ALTER COLUMN permit_expiry DROP NOT NULL;
-- +goose Down
UPDATE vehicles SET insurance_expiry = CURRENT_DATE + INTERVAL '1 year' WHERE insurance_expiry IS NULL;
UPDATE vehicles SET fitness_expiry = CURRENT_DATE + INTERVAL '1 year' WHERE fitness_expiry IS NULL;
UPDATE vehicles SET permit_expiry = CURRENT_DATE + INTERVAL '1 year' WHERE permit_expiry IS NULL;
ALTER TABLE vehicles ALTER COLUMN insurance_expiry SET NOT NULL;
ALTER TABLE vehicles ALTER COLUMN fitness_expiry SET NOT NULL;
ALTER TABLE vehicles ALTER COLUMN permit_expiry SET NOT NULL;
