-- +goose Up
-- 00136 — backfill L9: convert legacy '' vehicle_id sentinels on
-- telemetry_positions to NULL. New rows store NULL since the ingest fix;
-- historical rows from unbound-device frames predate it. Readers filter
-- `IS NOT NULL AND != ''`, so NULL is excluded identically — pure hygiene,
-- zero behavior change. Idempotent (no-op when no sentinels exist).
UPDATE telemetry_positions SET vehicle_id = NULL WHERE vehicle_id = '';
-- +goose Down
-- Down is a no-op by design: '' and NULL read identically everywhere, so
-- restoring the sentinel would be churn with no consumer.
SELECT 1;
