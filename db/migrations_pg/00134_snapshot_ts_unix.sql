-- PG port of 00134_snapshot_ts_unix.sql
-- +goose Up
ALTER TABLE telemetry_snapshots ADD COLUMN ts_unix BIGINT;
UPDATE telemetry_snapshots SET ts_unix = EXTRACT(EPOCH FROM timestamp)::BIGINT WHERE ts_unix IS NULL;
CREATE INDEX IF NOT EXISTS idx_telemetry_snapshots_vehicle_ts ON telemetry_snapshots(vehicle_id, ts_unix);
-- +goose Down
DROP INDEX IF EXISTS idx_telemetry_snapshots_vehicle_ts;
ALTER TABLE telemetry_snapshots DROP COLUMN ts_unix;
