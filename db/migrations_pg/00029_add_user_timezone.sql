-- PG port of 00029_add_user_timezone.sql | status: PORTABLE | flags: none
-- +goose Up
ALTER TABLE users ADD COLUMN timezone TEXT NOT NULL DEFAULT 'Asia/Kolkata';
-- +goose Down
ALTER TABLE users DROP COLUMN timezone;
