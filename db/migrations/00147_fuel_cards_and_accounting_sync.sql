-- +goose Up
-- 00147: fuel_cards_and_accounting_sync — Commercial Fuel Cards & Accounting Integration (Spec 20 §2, B10).

CREATE TABLE IF NOT EXISTS fuel_cards (
    id                  TEXT PRIMARY KEY,
    tenant_id           TEXT NOT NULL,
    card_number_masked  TEXT NOT NULL,
    card_token_hash     TEXT NOT NULL,
    provider            TEXT NOT NULL CHECK (provider IN ('IOCL', 'BPCL', 'HPCL', 'SHELL', 'FLEET_BANK')),
    assigned_vehicle_id TEXT REFERENCES vehicles(id) ON DELETE SET NULL,
    assigned_driver_id  TEXT REFERENCES drivers(id) ON DELETE SET NULL,
    daily_spend_limit   REAL NOT NULL DEFAULT 50000 CHECK (daily_spend_limit >= 0),
    status              TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'SUSPENDED', 'CANCELLED')),
    created_at          DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at          DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
    UNIQUE (tenant_id, card_token_hash)
);

CREATE INDEX IF NOT EXISTS idx_fuel_cards_tenant
    ON fuel_cards(tenant_id, status);

CREATE TABLE IF NOT EXISTS fuel_card_transactions (
    id                    TEXT PRIMARY KEY,
    tenant_id             TEXT NOT NULL,
    fuel_card_id          TEXT NOT NULL REFERENCES fuel_cards(id) ON DELETE CASCADE,
    external_txn_id       TEXT NOT NULL,
    txn_time              DATETIME NOT NULL,
    fuel_station_name     TEXT NOT NULL,
    fuel_station_city     TEXT,
    fuel_type             TEXT NOT NULL DEFAULT 'DIESEL' CHECK (fuel_type IN ('DIESEL', 'PETROL', 'CNG', 'DEF', 'LUBRICANT')),
    volume_litres         REAL NOT NULL CHECK (volume_litres > 0),
    rate_per_litre        REAL NOT NULL CHECK (rate_per_litre > 0),
    total_amount          REAL NOT NULL CHECK (total_amount > 0),
    odometer_reported     REAL,
    reconciliation_status TEXT NOT NULL DEFAULT 'UNRECONCILED'
                          CHECK (reconciliation_status IN ('UNRECONCILED', 'MATCHED_EXPENSE', 'SYSTEM_GENERATED', 'FLAGGED_ANOMALY')),
    matched_expense_id    TEXT REFERENCES driver_expenses(id) ON DELETE SET NULL,
    sync_log_id           TEXT REFERENCES accounting_sync_log(id) ON DELETE SET NULL,
    notes                 TEXT,
    created_at            DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
    UNIQUE (tenant_id, external_txn_id)
);

CREATE INDEX IF NOT EXISTS idx_fuel_txns_tenant_time
    ON fuel_card_transactions(tenant_id, txn_time);
CREATE INDEX IF NOT EXISTS idx_fuel_txns_recon
    ON fuel_card_transactions(tenant_id, reconciliation_status);
CREATE INDEX IF NOT EXISTS idx_fuel_txns_card
    ON fuel_card_transactions(tenant_id, fuel_card_id);

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_fuel_cards_tenant_fk_insert
BEFORE INSERT ON fuel_cards
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for fuel_cards.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_fuel_cards_tenant_fk_update
BEFORE UPDATE OF tenant_id ON fuel_cards
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for fuel_cards.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_fuel_txns_tenant_fk_insert
BEFORE INSERT ON fuel_card_transactions
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for fuel_card_transactions.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_fuel_txns_tenant_fk_update
BEFORE UPDATE OF tenant_id ON fuel_card_transactions
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for fuel_card_transactions.tenant_id') END;
END;
-- +goose StatementEnd

-- RBAC seed: fuel:write
INSERT OR IGNORE INTO permissions (name, description) VALUES
    ('fuel:write', 'Create and manage fuel cards and transactions');

INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT 1, id FROM permissions WHERE name = 'fuel:write';

INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT 2, id FROM permissions WHERE name = 'fuel:write';

-- +goose Down
-- +goose StatementBegin
DELETE FROM role_permissions WHERE permission_id IN (
    SELECT id FROM permissions WHERE name = 'fuel:write'
);
DELETE FROM permissions WHERE name = 'fuel:write';
DROP TRIGGER IF EXISTS trg_fuel_txns_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_fuel_txns_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_fuel_cards_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_fuel_cards_tenant_fk_insert;
DROP INDEX IF EXISTS idx_fuel_txns_card;
DROP INDEX IF EXISTS idx_fuel_txns_recon;
DROP INDEX IF EXISTS idx_fuel_txns_tenant_time;
DROP TABLE IF EXISTS fuel_card_transactions;
DROP INDEX IF EXISTS idx_fuel_cards_tenant;
DROP TABLE IF EXISTS fuel_cards;
-- +goose StatementEnd
