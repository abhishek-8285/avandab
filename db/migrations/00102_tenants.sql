-- +goose Up
CREATE TABLE tenants (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    slug       TEXT UNIQUE,
    status     TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','suspended')),
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
INSERT INTO tenants (id, name, slug) VALUES ('1', 'Default', 'default');
ALTER TABLE users ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '1';
CREATE INDEX idx_users_tenant ON users(tenant_id);
INSERT OR IGNORE INTO permissions (name, description) VALUES ('tenants:manage', 'Create and suspend tenant organizations');
INSERT OR IGNORE INTO role_permissions (role_id, permission_id) SELECT 1, id FROM permissions WHERE name = 'tenants:manage';
-- +goose Down
DELETE FROM role_permissions WHERE permission_id = (SELECT id FROM permissions WHERE name='tenants:manage');
DELETE FROM permissions WHERE name='tenants:manage';
DROP INDEX IF EXISTS idx_users_tenant;
DROP TABLE IF EXISTS tenants;
ALTER TABLE users DROP COLUMN tenant_id;
