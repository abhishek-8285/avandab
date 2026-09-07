-- PG port of 00036_remove_seed_admin.sql | status: PORTABLE | flags: none
-- +goose Up
-- Neutralize the legacy default seed admin user if it exists.
UPDATE users
SET status = 'inactive',
    password_hash = '$2a$04$invaliddisabledaccountplaceholder'
WHERE email = 'admin@transport.local';
-- +goose Down
SELECT 1;
