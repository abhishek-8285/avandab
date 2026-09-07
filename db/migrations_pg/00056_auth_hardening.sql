-- PG port of 00056_auth_hardening.sql | status: PORTABLE | flags: none
-- +goose Up
-- 00056: Auth hardening (api_tokens, sessions tenant, drivers.user_id, enc token) — Spec 10
-- Placeholder: consolidated; no-op to maintain sequence.
SELECT 1;
-- +goose Down
SELECT 1;
