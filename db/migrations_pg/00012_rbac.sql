-- PG port of 00012_rbac.sql | status: MANUAL | flags: MANUAL-TRIGGER | reviewed: YES
-- +goose Up
-- Create normalized tables for RBAC
CREATE TABLE role_permissions (
    role_id       INTEGER NOT NULL,
    permission_id INTEGER NOT NULL,
    PRIMARY KEY (role_id, permission_id),
    FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE CASCADE,
    FOREIGN KEY (permission_id) REFERENCES permissions(id) ON DELETE CASCADE
);
CREATE TABLE user_roles (
    user_id       TEXT NOT NULL,
    role_id       INTEGER NOT NULL,
    PRIMARY KEY (user_id, role_id),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE CASCADE
);
-- Sync existing users to user_roles
INSERT INTO user_roles (user_id, role_id)
SELECT id, role_id FROM users ON CONFLICT DO NOTHING;
-- Seed permissions
-- Resources: drivers, vehicles, customers, routes, bookings, trips, invoices, payments, reports, settings, users
-- Actions: create, read, update, delete, export, assign, approve, cancel
INSERT INTO permissions (name, description) VALUES
('users:create', 'Create users'),
('users:read', 'Read users'),
('users:update', 'Update users'),
('users:delete', 'Delete users'),

('drivers:create', 'Create drivers'),
('drivers:read', 'Read drivers'),
('drivers:update', 'Update drivers'),
('drivers:delete', 'Delete drivers'),
('drivers:export', 'Export drivers'),

('vehicles:create', 'Create vehicles'),
('vehicles:read', 'Read vehicles'),
('vehicles:update', 'Update vehicles'),
('vehicles:delete', 'Delete vehicles'),
('vehicles:export', 'Export vehicles'),

('customers:create', 'Create customers'),
('customers:read', 'Read customers'),
('customers:update', 'Update customers'),
('customers:delete', 'Delete customers'),
('customers:export', 'Export customers'),

('routes:create', 'Create routes'),
('routes:read', 'Read routes'),
('routes:update', 'Update routes'),
('routes:delete', 'Delete routes'),

('bookings:create', 'Create bookings'),
('bookings:read', 'Read bookings'),
('bookings:update', 'Update bookings'),
('bookings:delete', 'Delete bookings'),
('bookings:cancel', 'Cancel bookings'),
('bookings:approve', 'Approve bookings'),

('trips:create', 'Create trips'),
('trips:read', 'Read trips'),
('trips:update', 'Update trips'),
('trips:delete', 'Delete trips'),
('trips:assign', 'Assign drivers/vehicles to trips'),

('invoices:create', 'Create invoices'),
('invoices:read', 'Read invoices'),
('invoices:update', 'Update invoices'),
('invoices:delete', 'Delete invoices'),
('invoices:export', 'Export invoices'),

('payments:create', 'Create payments'),
('payments:read', 'Read payments'),
('payments:update', 'Update payments'),
('payments:delete', 'Delete payments'),

('reports:read', 'Read reports'),
('reports:export', 'Export reports'),

('settings:read', 'Read company settings'),
('settings:update', 'Update company settings'),

('audit_logs:read', 'Read audit logs'),

('files:create', 'Upload files'),
('files:read', 'Read files'),
('files:delete', 'Delete files') ON CONFLICT DO NOTHING;
-- Helper to assign all permissions of a resource to admin (id: 1)
INSERT INTO role_permissions (role_id, permission_id)
SELECT 1, id FROM permissions ON CONFLICT DO NOTHING;
-- Assign Dispatcher (id: 2) permissions
-- Can manage drivers, vehicles, customers, routes, bookings, trips, and view reports/settings
INSERT INTO role_permissions (role_id, permission_id)
SELECT 2, id FROM permissions 
WHERE name LIKE 'drivers:%' 
   OR name LIKE 'vehicles:%' 
   OR name LIKE 'customers:%' 
   OR name LIKE 'routes:%' 
   OR name LIKE 'bookings:%' 
   OR name LIKE 'trips:%' 
   OR name IN ('reports:read', 'reports:export', 'settings:read', 'audit_logs:read') ON CONFLICT DO NOTHING;
-- Assign Accountant (id: 3) permissions
-- Can manage invoices, payments, and view reports/settings
INSERT INTO role_permissions (role_id, permission_id)
SELECT 3, id FROM permissions 
WHERE name LIKE 'invoices:%' 
   OR name LIKE 'payments:%' 
   OR name IN ('reports:read', 'reports:export', 'settings:read') ON CONFLICT DO NOTHING;
-- Assign Viewer (id: 4) permissions
-- Read-only access to almost everything, no access to user settings or users
INSERT INTO role_permissions (role_id, permission_id)
SELECT 4, id FROM permissions 
WHERE name LIKE '%:read'
  AND name NOT LIKE 'users:%'
  AND name NOT LIKE 'settings:%' ON CONFLICT DO NOTHING;
-- Triggers to keep user_roles synchronized with users table (PG: plpgsql;
-- sqlite uses inline BEGIN/END triggers — see db/migrations/00012_rbac.sql)
DROP TRIGGER IF EXISTS sync_user_role_on_insert ON users;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION sync_user_role_on_insert_fn()
RETURNS trigger AS $$
BEGIN
    INSERT INTO user_roles (user_id, role_id)
    VALUES (NEW.id, NEW.role_id)
    ON CONFLICT (user_id, role_id) DO NOTHING;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
CREATE TRIGGER sync_user_role_on_insert
AFTER INSERT ON users
FOR EACH ROW EXECUTE FUNCTION sync_user_role_on_insert_fn();
DROP TRIGGER IF EXISTS sync_user_role_on_update ON users;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION sync_user_role_on_update_fn()
RETURNS trigger AS $$
BEGIN
    DELETE FROM user_roles WHERE user_id = OLD.id;
    INSERT INTO user_roles (user_id, role_id)
    VALUES (NEW.id, NEW.role_id);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
CREATE TRIGGER sync_user_role_on_update
AFTER UPDATE OF role_id ON users
FOR EACH ROW EXECUTE FUNCTION sync_user_role_on_update_fn();
DROP TRIGGER IF EXISTS sync_user_role_on_delete ON users;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION sync_user_role_on_delete_fn()
RETURNS trigger AS $$
BEGIN
    DELETE FROM user_roles WHERE user_id = OLD.id;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
CREATE TRIGGER sync_user_role_on_delete
AFTER DELETE ON users
FOR EACH ROW EXECUTE FUNCTION sync_user_role_on_delete_fn();
-- +goose Down
DROP TRIGGER IF EXISTS sync_user_role_on_insert ON users;
DROP TRIGGER IF EXISTS sync_user_role_on_update ON users;
DROP TRIGGER IF EXISTS sync_user_role_on_delete ON users;
DROP FUNCTION IF EXISTS sync_user_role_on_insert_fn();
DROP FUNCTION IF EXISTS sync_user_role_on_update_fn();
DROP FUNCTION IF EXISTS sync_user_role_on_delete_fn();
DROP TABLE IF EXISTS user_roles;
DROP TABLE IF EXISTS role_permissions;
DELETE FROM permissions WHERE name LIKE '%:%';
