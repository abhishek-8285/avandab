-- +goose Up
-- SQL in this section is executed after the migration is applied.

CREATE TABLE IF NOT EXISTS driver_push_tokens (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    driver_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    device_id TEXT NOT NULL,
    push_token TEXT NOT NULL,
    platform TEXT NOT NULL DEFAULT 'android',
    is_active INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_driver_push_tokens_device ON driver_push_tokens(tenant_id, driver_id, device_id);
CREATE INDEX IF NOT EXISTS idx_driver_push_tokens_driver ON driver_push_tokens(tenant_id, driver_id);
CREATE INDEX IF NOT EXISTS idx_driver_push_tokens_active ON driver_push_tokens(is_active);

-- +goose Down
-- SQL in this section is executed when the migration is rolled back.

DROP TABLE IF EXISTS driver_push_tokens;
