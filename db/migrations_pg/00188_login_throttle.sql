-- +goose Up
-- PG port of 00188_login_throttle.sql
-- Per-account login throttle. key is sha256(lower(trim(email))).

CREATE TABLE IF NOT EXISTS login_throttle (
    email_key    TEXT PRIMARY KEY,
    failures     INTEGER NOT NULL DEFAULT 0,
    locked_until TIMESTAMPTZ,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_login_throttle_updated
    ON login_throttle(updated_at);

-- +goose Down
DROP INDEX IF EXISTS idx_login_throttle_updated;
DROP TABLE IF EXISTS login_throttle;
