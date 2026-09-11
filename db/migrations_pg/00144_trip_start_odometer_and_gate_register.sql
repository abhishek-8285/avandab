-- PG port of 00144_trip_start_odometer_and_gate_register.sql
-- +goose Up
-- 00144 — TMS SOP ZMOTM_MR Gate Register parity (pp.16-20):
-- Captures start odometer reading and departure facility for depot in/out gate registration.
ALTER TABLE trips ADD COLUMN start_odometer DOUBLE PRECISION;
ALTER TABLE trips ADD COLUMN gate_facility_id TEXT;

-- +goose Down
ALTER TABLE trips DROP COLUMN gate_facility_id;
ALTER TABLE trips DROP COLUMN start_odometer;
