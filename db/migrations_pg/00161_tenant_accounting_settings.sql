-- +goose Up
-- 00161: per-tenant accounting provider choice — PG port of 00161_tenant_accounting_settings.sql
-- Note: Postgres uses timestamptz + direct REFERENCES; SQLite uses triggers.

CREATE TABLE IF NOT EXISTS tenant_accounting_settings (
    tenant_id  TEXT PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    provider   TEXT NOT NULL DEFAULT 'none'
               CHECK (provider IN ('none','tally','zoho','busy_excel','excel')),
    endpoint   TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +goose Down
DROP TABLE IF EXISTS tenant_accounting_settings;
