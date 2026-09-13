-- +goose Up
-- 00152 — KMPL SOP report set (ZMOTM_MR p.19 follow-up, B13):
-- Per-vehicle standard (norm) KMPL for variance flagging in the KMPL summary
-- report. NULL = no norm set, no variance computed. Must be > 0 when set.
ALTER TABLE vehicles ADD COLUMN standard_kmpl REAL CHECK (standard_kmpl IS NULL OR standard_kmpl > 0);

-- +goose Down
ALTER TABLE vehicles DROP COLUMN standard_kmpl;
