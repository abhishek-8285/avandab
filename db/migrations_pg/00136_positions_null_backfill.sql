-- PG port of 00136_positions_null_backfill.sql
-- +goose Up
UPDATE telemetry_positions SET vehicle_id = NULL WHERE vehicle_id = '';
-- +goose Down
-- No-op by design (see sqlite migration): '' and NULL read identically.
SELECT 1;
