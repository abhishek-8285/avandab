-- PG port of 00133_user_email_verified.sql
-- +goose Up
ALTER TABLE users ADD COLUMN email_verified_at TIMESTAMPTZ;
-- +goose Down
ALTER TABLE users DROP COLUMN email_verified_at;
