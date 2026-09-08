-- PG port of 00131_driver_license_nullable.sql
-- +goose Up
-- 00131 — drivers.license_number / license_expiry go nullable (unknown license
-- is NULL, never a 'DL-PENDING' + invented +5y placeholder).
-- Native ALTER (no rebuild needed on Postgres).
ALTER TABLE drivers ALTER COLUMN license_number DROP NOT NULL;
ALTER TABLE drivers ALTER COLUMN license_expiry DROP NOT NULL;
-- Fabricated placeholders become honest NULLs (number and expiry alike: the
-- expiry on a DL-PENDING row was always invented at registration time).
UPDATE drivers SET license_number = NULL, license_expiry = NULL WHERE license_number = 'DL-PENDING';
-- +goose Down
UPDATE drivers SET license_number = 'DL-PENDING', license_expiry = CURRENT_DATE + INTERVAL '5 years' WHERE license_number IS NULL OR license_expiry IS NULL;
ALTER TABLE drivers ALTER COLUMN license_number SET NOT NULL;
ALTER TABLE drivers ALTER COLUMN license_expiry SET NOT NULL;
