-- +goose Up
-- 00163: delete dead company_config accounting seeds (00050). Nothing ever
-- read them (live path is env vars; per-tenant choice is 00161
-- tenant_accounting_settings) and the tenant-'1' mock/false values would
-- override env config for single-tenant deployments if anyone wired them
-- as fallback in future. Down restores the seeds (rollback-safe).

DELETE FROM company_config
WHERE tenant_id = '1' AND key IN (
    'accounting_adapter', 'accounting_enabled',
    'accounting_endpoint', 'accounting_api_key'
);

-- +goose Down
INSERT OR IGNORE INTO company_config (tenant_id, key, value) VALUES
('1', 'accounting_adapter', 'mock'),
('1', 'accounting_enabled', 'false'),
('1', 'accounting_endpoint', ''),
('1', 'accounting_api_key', '');
