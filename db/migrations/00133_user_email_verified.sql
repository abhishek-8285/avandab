-- +goose Up
-- 00133 — users.email_verified_at (NULL = unverified). Set when a
-- verification link is consumed, or at Google OAuth sign-in (Google only
-- hands over verified emails — the handler rejects the rest). Badge only;
-- nothing gates on it, so no lockout risk.
ALTER TABLE users ADD COLUMN email_verified_at DATETIME;
-- +goose Down
ALTER TABLE users DROP COLUMN email_verified_at;
