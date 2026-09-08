-- PG port of 00129_trip_close_reading.sql
-- +goose Up
-- 00129 — TMS SOP trip close parity: trips.close_odometer (close reading).
-- Native ALTER (no rebuild needed on Postgres).
ALTER TABLE trips ADD COLUMN close_odometer DOUBLE PRECISION;
-- +goose Down
ALTER TABLE trips DROP COLUMN close_odometer;
