-- +goose Up
-- 00134 — telemetry_snapshots.ts_unix (epoch seconds, NULL when unknown).
-- The timestamp TEXT column holds mixed layouts (space-separated fixtures
-- vs Go-String pipeline rows with a "+0000 UTC" suffix), which SQLite
-- datetime()/strftime() parse to NULL — any SQL-side time math silently
-- misbehaves. ts_unix is the canonical machine-readable clock: the pipeline
-- writes it on every insert; the backfill covers rows strftime understands
-- and honestly leaves the rest NULL (Go-side readers keep parsing the text
-- column, which database/sql handles for every layout).
ALTER TABLE telemetry_snapshots ADD COLUMN ts_unix INTEGER;
UPDATE telemetry_snapshots SET ts_unix = CAST(strftime('%s', timestamp) AS INTEGER) WHERE ts_unix IS NULL;
CREATE INDEX IF NOT EXISTS idx_telemetry_snapshots_vehicle_ts ON telemetry_snapshots(vehicle_id, ts_unix);
-- +goose Down
DROP INDEX IF EXISTS idx_telemetry_snapshots_vehicle_ts;
ALTER TABLE telemetry_snapshots DROP COLUMN ts_unix;
