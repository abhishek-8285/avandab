-- +goose Up
-- Internal append-only money ledger (double-entry-lite). One immutable row
-- per money movement (invoice generated, payment recorded, driver settlement,
-- kharcha approved, manual adjustment). Amounts are integer minor units
-- (paise); sign never lives in amount_minor — direction carries it.
CREATE TABLE IF NOT EXISTS money_ledger (
    id           TEXT PRIMARY KEY,
    tenant_id    TEXT NOT NULL,
    txn_type     TEXT NOT NULL CHECK (txn_type IN ('invoice_generated','payment_recorded','settlement_recorded','kharcha_approved','adjustment')),
    ref_table    TEXT NOT NULL,
    ref_id       TEXT NOT NULL,
    direction    TEXT NOT NULL CHECK (direction IN ('debit','credit')),
    amount_minor INTEGER NOT NULL CHECK (amount_minor >= 0),
    currency     TEXT NOT NULL DEFAULT 'INR',
    memo         TEXT,
    created_by   TEXT,
    created_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_money_ledger_tenant_created ON money_ledger(tenant_id, created_at);
CREATE INDEX IF NOT EXISTS idx_money_ledger_ref ON money_ledger(ref_table, ref_id);

-- +goose Down
DROP INDEX IF EXISTS idx_money_ledger_ref;
DROP INDEX IF EXISTS idx_money_ledger_tenant_created;
DROP TABLE IF EXISTS money_ledger;
