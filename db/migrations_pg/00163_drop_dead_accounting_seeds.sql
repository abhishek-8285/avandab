-- +goose Up
-- 00163: delete dead company_config accounting seeds — PG port of 00163_drop_dead_accounting_seeds.sql
-- Down restores the seeds (rollback-safe).

DELETE FROM company_config
WHERE tenant_id = '1' AND key IN (
    'accounting_adapter', 'accounting_enabled',
    'accounting_endpoint', 'accounting_api_key'
);

-- +goose Down
INSERT INTO company_config (tenant_id, key, value) VALUES
('1', 'accounting_adapter', 'mock'),
('1', 'accounting_enabled', 'false'),
('1', 'accounting_endpoint', ''),
('1', 'accounting_api_key', '')
ON CONFLICT DO NOTHING;
