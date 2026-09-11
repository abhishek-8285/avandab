-- PG port of 00147_fuel_cards_and_accounting_sync.sql | status: PORTABLE | flags: none
-- +goose Up

CREATE TABLE IF NOT EXISTS fuel_cards (
    id                  TEXT PRIMARY KEY,
    tenant_id           TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    card_number_masked  TEXT NOT NULL,
    card_token_hash     TEXT NOT NULL,
    provider            TEXT NOT NULL CHECK (provider IN ('IOCL', 'BPCL', 'HPCL', 'SHELL', 'FLEET_BANK')),
    assigned_vehicle_id TEXT REFERENCES vehicles(id) ON DELETE SET NULL,
    assigned_driver_id  TEXT REFERENCES drivers(id) ON DELETE SET NULL,
    daily_spend_limit   DOUBLE PRECISION NOT NULL DEFAULT 50000 CHECK (daily_spend_limit >= 0),
    status              TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'SUSPENDED', 'CANCELLED')),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, card_token_hash)
);

CREATE INDEX IF NOT EXISTS idx_fuel_cards_tenant
    ON fuel_cards(tenant_id, status);

CREATE TABLE IF NOT EXISTS fuel_card_transactions (
    id                    TEXT PRIMARY KEY,
    tenant_id             TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    fuel_card_id          TEXT NOT NULL REFERENCES fuel_cards(id) ON DELETE CASCADE,
    external_txn_id       TEXT NOT NULL,
    txn_time              TIMESTAMPTZ NOT NULL,
    fuel_station_name     TEXT NOT NULL,
    fuel_station_city     TEXT,
    fuel_type             TEXT NOT NULL DEFAULT 'DIESEL' CHECK (fuel_type IN ('DIESEL', 'PETROL', 'CNG', 'DEF', 'LUBRICANT')),
    volume_litres         DOUBLE PRECISION NOT NULL CHECK (volume_litres > 0),
    rate_per_litre        DOUBLE PRECISION NOT NULL CHECK (rate_per_litre > 0),
    total_amount          DOUBLE PRECISION NOT NULL CHECK (total_amount > 0),
    odometer_reported     DOUBLE PRECISION,
    reconciliation_status TEXT NOT NULL DEFAULT 'UNRECONCILED'
                          CHECK (reconciliation_status IN ('UNRECONCILED', 'MATCHED_EXPENSE', 'SYSTEM_GENERATED', 'FLAGGED_ANOMALY')),
    matched_expense_id    TEXT REFERENCES driver_expenses(id) ON DELETE SET NULL,
    sync_log_id           TEXT REFERENCES accounting_sync_log(id) ON DELETE SET NULL,
    notes                 TEXT,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, external_txn_id)
);

CREATE INDEX IF NOT EXISTS idx_fuel_txns_tenant_time
    ON fuel_card_transactions(tenant_id, txn_time);
CREATE INDEX IF NOT EXISTS idx_fuel_txns_recon
    ON fuel_card_transactions(tenant_id, reconciliation_status);
CREATE INDEX IF NOT EXISTS idx_fuel_txns_card
    ON fuel_card_transactions(tenant_id, fuel_card_id);

-- RBAC seed: fuel:write
INSERT INTO permissions (name, description) VALUES
    ('fuel:write', 'Create and manage fuel cards and transactions')
ON CONFLICT (name) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT 1, id FROM permissions WHERE name = 'fuel:write'
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT 2, id FROM permissions WHERE name = 'fuel:write'
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN (
    SELECT id FROM permissions WHERE name = 'fuel:write'
);
DELETE FROM permissions WHERE name = 'fuel:write';
DROP INDEX IF EXISTS idx_fuel_txns_card;
DROP INDEX IF EXISTS idx_fuel_txns_recon;
DROP INDEX IF EXISTS idx_fuel_txns_tenant_time;
DROP TABLE IF EXISTS fuel_card_transactions;
DROP INDEX IF EXISTS idx_fuel_cards_tenant;
DROP TABLE IF EXISTS fuel_cards;
