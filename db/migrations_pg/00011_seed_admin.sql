-- PG port of 00011_seed_admin.sql | status: PORTABLE | flags: none
-- +goose Up
-- The default admin is no longer seeded with a hardcoded password.
-- Create the initial admin at startup via BOOTSTRAP_ADMIN_EMAIL /
-- BOOTSTRAP_ADMIN_NAME / BOOTSTRAP_ADMIN_PASSWORD environment variables.
-- Migration 00035 removes any previously seeded admin that still uses the
-- known default credentials.
SELECT 1;
-- +goose Down
DELETE FROM users WHERE email = 'admin@transport.local';
