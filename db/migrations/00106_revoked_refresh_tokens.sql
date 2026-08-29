-- +goose Up
CREATE TABLE IF NOT EXISTS revoked_refresh_tokens (
    token_hash TEXT PRIMARY KEY,
    revoked_at DATETIME NOT NULL DEFAULT (datetime('now')),
    user_id TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_revoked_refresh_user ON revoked_refresh_tokens(user_id);

-- +goose Down
DROP TABLE IF EXISTS revoked_refresh_tokens;
