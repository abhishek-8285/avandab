-- PG port of 00053_outbox_correction.sql | status: PORTABLE | flags: none
-- +goose Up
-- 00053: Event bus / outbox correction (status/attempts/last_error) — Spec 09
-- Placeholder: original spec migration was consolidated into 00020/00045.
-- This no-op preserves goose sequence idempotency (gap 52->57).
SELECT 1;
-- +goose Down
SELECT 1;
