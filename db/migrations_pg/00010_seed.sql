-- PG port of 00010_seed.sql | status: PORTABLE | flags: none
-- +goose Up
-- Seed roles
INSERT INTO roles (name, description) VALUES ('admin', 'Full system administrator') ON CONFLICT DO NOTHING;
INSERT INTO roles (name, description) VALUES ('dispatcher', 'Manages bookings and trips') ON CONFLICT DO NOTHING;
INSERT INTO roles (name, description) VALUES ('accountant', 'Handles invoices and payments') ON CONFLICT DO NOTHING;
INSERT INTO roles (name, description) VALUES ('viewer', 'Read-only access') ON CONFLICT DO NOTHING;
-- Seed default company settings
INSERT INTO company_settings (id, company_name) VALUES (1, 'Transport Company') ON CONFLICT DO NOTHING;
-- +goose Down
DELETE FROM company_settings WHERE id = 1;
DELETE FROM roles WHERE name IN ('admin', 'dispatcher', 'accountant', 'viewer');
