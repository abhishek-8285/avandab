-- +goose Up
-- 00161: per-tenant accounting provider choice (user decision, tenant-isolated).
-- Tenant picks none|tally|zoho|busy_excel|excel in Settings; reads resolve
-- tenant row first, env/global config fallback. Live push only for tally;
-- zoho/busy_excel/excel stay CSV-import workflow (adapters mock-only).

CREATE TABLE IF NOT EXISTS tenant_accounting_settings (
    tenant_id  TEXT PRIMARY KEY,
    provider   TEXT NOT NULL DEFAULT 'none'
               CHECK (provider IN ('none','tally','zoho','busy_excel','excel')),
    endpoint   TEXT NOT NULL DEFAULT '',
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_tenant_accounting_settings_fk_insert
BEFORE INSERT ON tenant_accounting_settings
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for tenant_accounting_settings.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_tenant_accounting_settings_fk_update
BEFORE UPDATE OF tenant_id ON tenant_accounting_settings
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for tenant_accounting_settings.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS trg_tenant_accounting_settings_fk_update;
DROP TRIGGER IF EXISTS trg_tenant_accounting_settings_fk_insert;
DROP TABLE IF EXISTS tenant_accounting_settings;
