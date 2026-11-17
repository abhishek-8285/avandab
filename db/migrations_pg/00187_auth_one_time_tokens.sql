-- +goose Up
-- PG port of 00187_auth_one_time_tokens.sql
-- Persistent one-time tokens (password reset + email verification).
-- Only the HMAC hash of the token value is stored; the email address is
-- stored as the lookup key (PII).

CREATE TABLE IF NOT EXISTS auth_one_time_tokens (
    token_hash TEXT PRIMARY KEY,
    email      TEXT NOT NULL,
    purpose    TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_auth_one_time_tokens_purpose_expires
    ON auth_one_time_tokens(purpose, expires_at);

-- +goose Down
DROP INDEX IF EXISTS idx_auth_one_time_tokens_purpose_expires;
DROP TABLE IF EXISTS auth_one_time_tokens;
