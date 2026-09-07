-- PG port of 00050_accounting_sync.sql | status: PORTABLE | flags: none
-- +goose Up
-- 00050 Accounting two-way sync: sync log, entity mapping, GL rules, company_config seeds

CREATE TABLE IF NOT EXISTS accounting_sync_log (
    id               TEXT PRIMARY KEY,
    idempotency_key  TEXT NOT NULL UNIQUE,
    direction        TEXT NOT NULL CHECK (direction IN ('out','in')),
    entity_type      TEXT NOT NULL,
    entity_id        TEXT NOT NULL,
    adapter          TEXT NOT NULL,
    payload_json     TEXT NOT NULL,
    external_id      TEXT,
    status           TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','sent','acked','failed')),
    attempts         INTEGER NOT NULL DEFAULT 0,
    last_error       TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_accsync_idem ON accounting_sync_log(idempotency_key);
CREATE INDEX IF NOT EXISTS idx_accsync_status ON accounting_sync_log(status);
CREATE TABLE IF NOT EXISTS accounting_mapping (
    id            TEXT PRIMARY KEY,
    entity_type   TEXT NOT NULL,
    entity_id     TEXT NOT NULL,
    adapter       TEXT NOT NULL,
    external_id   TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (entity_type, entity_id, adapter)
);
CREATE INDEX IF NOT EXISTS idx_accmap_entity ON accounting_mapping(entity_type, entity_id);
CREATE TABLE IF NOT EXISTS accounting_gl_rule (
    id            TEXT PRIMARY KEY,
    event_type    TEXT NOT NULL,
    debit_account TEXT NOT NULL,
    credit_account TEXT NOT NULL,
    description   TEXT,
    priority      INTEGER NOT NULL DEFAULT 0
);
INSERT INTO accounting_gl_rule (id, event_type, debit_account, credit_account, description, priority) VALUES
('gl_payout',  'DriverPayoutSettled', 'Driver Payable',   'Bank - Current', 'Driver net payout', 10),
('gl_inv',     'InvoiceExported',     'Accounts Receivable','Service Revenue', 'Customer invoice', 10),
('gl_tds',     'TDSRemitted',         'TDS Payable',      'Driver Payable',  'TDS withheld',     20) ON CONFLICT DO NOTHING;
INSERT INTO company_config (tenant_id, key, value) VALUES
('1', 'accounting_adapter', 'mock'),
('1', 'accounting_enabled', 'false'),
('1', 'accounting_endpoint', ''),
('1', 'accounting_api_key', '') ON CONFLICT DO NOTHING;
-- +goose Down
DROP TABLE IF EXISTS accounting_gl_rule;
DROP TABLE IF EXISTS accounting_mapping;
DROP TABLE IF EXISTS accounting_sync_log;
DELETE FROM company_config WHERE key IN
('accounting_adapter','accounting_enabled','accounting_endpoint','accounting_api_key');
