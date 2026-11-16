-- +goose Up
-- 00181 — persistent one-time tokens (password reset + email verification).
-- Audit 2026-11-12 M1: tokens lived only in process memory, so a restart
-- invalidated every outstanding link and multi-replica deployments rejected
-- links issued by another replica. Only the HMAC hash is stored.

CREATE TABLE IF NOT EXISTS auth_one_time_tokens (
    token_hash TEXT PRIMARY KEY,
    email      TEXT NOT NULL,
    purpose    TEXT NOT NULL,
    expires_at DATETIME NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_auth_one_time_tokens_purpose_expires
    ON auth_one_time_tokens(purpose, expires_at);

-- +goose Down
DROP INDEX IF EXISTS idx_auth_one_time_tokens_purpose_expires;
DROP TABLE IF EXISTS auth_one_time_tokens;
