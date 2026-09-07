-- PG port of 00102_tenants.sql | status: PORTABLE | flags: none
-- +goose Up
CREATE TABLE tenants (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    slug       TEXT UNIQUE,
    status     TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','suspended')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT (CURRENT_TIMESTAMP),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT (CURRENT_TIMESTAMP)
);
INSERT INTO tenants (id, name, slug) VALUES ('1', 'Default', 'default');
ALTER TABLE users ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '1';
CREATE INDEX idx_users_tenant ON users(tenant_id);
INSERT INTO permissions (name, description) VALUES ('tenants:manage', 'Create and suspend tenant organizations') ON CONFLICT DO NOTHING;
INSERT INTO role_permissions (role_id, permission_id) SELECT 1, id FROM permissions WHERE name = 'tenants:manage' ON CONFLICT DO NOTHING;
-- +goose Down
DELETE FROM role_permissions WHERE permission_id = (SELECT id FROM permissions WHERE name='tenants:manage');
DELETE FROM permissions WHERE name='tenants:manage';
DROP INDEX IF EXISTS idx_users_tenant;
DROP TABLE IF EXISTS tenants;
ALTER TABLE users DROP COLUMN tenant_id;
