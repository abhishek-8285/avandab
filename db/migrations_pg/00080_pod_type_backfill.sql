-- PG port of 00080_pod_type_backfill.sql | status: PORTABLE | flags: none
-- +goose Up
-- POD privacy backfill: before migration 00077 the files table CHECK constraint
-- had no trip_pod type, so ePOD delivery photos were stored as company_logo —
-- which DownloadFile treats as a public tenant asset. Re-type any file whose
-- uploadable_id references a real trip; genuine logos are untouched (they are
-- written without a files-table row or reference non-trip ids).
UPDATE files
SET uploadable_type = 'trip_pod'
WHERE uploadable_type = 'company_logo'
  AND uploadable_id IS NOT NULL
  AND uploadable_id IN (SELECT id FROM trips);
-- +goose Down
-- Best-effort reverse: only rows this migration could have touched (ids that
-- still match trips). Pre-existing true trip-typed logo rows would have been
-- indistinguishable, so down mirrors the same predicate.
UPDATE files
SET uploadable_type = 'company_logo'
WHERE uploadable_type = 'trip_pod'
  AND uploadable_id IS NOT NULL
  AND uploadable_id IN (SELECT id FROM trips);
