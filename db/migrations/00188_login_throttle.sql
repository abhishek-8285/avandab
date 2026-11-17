-- +goose Up
-- 00188 — per-account login throttle (audit 2026-11-12 M3).
-- Per-IP limits alone let credential stuffing from many IPs bypass /login.
-- key is sha256(lower(trim(email))) so raw addresses are not stored.

CREATE TABLE IF NOT EXISTS login_throttle (
    email_key    TEXT PRIMARY KEY,
    failures     INTEGER NOT NULL DEFAULT 0,
    locked_until DATETIME,
    updated_at   DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_login_throttle_updated
    ON login_throttle(updated_at);

-- +goose Down
DROP INDEX IF EXISTS idx_login_throttle_updated;
DROP TABLE IF EXISTS login_throttle;
