-- PG port of 00181_accounting_sync_tenant.sql
-- +goose Up
ALTER TABLE accounting_sync_log ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_accsync_tenant_status
    ON accounting_sync_log (tenant_id, status, created_at);

-- +goose Down
DROP INDEX IF EXISTS idx_accsync_tenant_status;
ALTER TABLE accounting_sync_log DROP COLUMN tenant_id;
