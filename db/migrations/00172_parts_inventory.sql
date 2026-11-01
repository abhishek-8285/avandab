-- +goose Up
-- 00172: Parts & Spares Inventory Management (FleetX Parity P3.2)
-- Introduces part catalog master, multi-store stock ledger linked to facilities,
-- purchase bills (GRN), part issue/return on work orders, and stock movements.

CREATE TABLE IF NOT EXISTS parts (
    id                 TEXT PRIMARY KEY,
    tenant_id          TEXT NOT NULL,
    part_number        TEXT NOT NULL,
    name               TEXT NOT NULL,
    category           TEXT NOT NULL DEFAULT 'GENERAL'
                       CHECK (category IN ('ENGINE', 'BRAKES', 'ELECTRICAL', 'SUSPENSION', 'TRANSMISSION', 'FILTERS', 'FLUIDS', 'TYRES_ACCESSORIES', 'FASTENERS', 'BODY', 'GENERAL')),
    description        TEXT NOT NULL DEFAULT '',
    uom                TEXT NOT NULL DEFAULT 'PIECES'
                       CHECK (uom IN ('PIECES', 'LITRES', 'METRES', 'SETS', 'KG', 'BOXES')),
    min_stock_level    REAL NOT NULL DEFAULT 5.0 CHECK (min_stock_level >= 0.0),
    reorder_quantity   REAL NOT NULL DEFAULT 10.0 CHECK (reorder_quantity >= 0.0),
    cost_price         REAL NOT NULL DEFAULT 0.0 CHECK (cost_price >= 0.0),
    selling_price      REAL NOT NULL DEFAULT 0.0 CHECK (selling_price >= 0.0),
    oem_brand          TEXT NOT NULL DEFAULT '',
    compatible_models  TEXT NOT NULL DEFAULT '',
    is_active          INTEGER NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),
    created_at         DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at         DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_parts_tenant_number
    ON parts(tenant_id, part_number);

CREATE INDEX IF NOT EXISTS idx_parts_tenant_category
    ON parts(tenant_id, category);

CREATE INDEX IF NOT EXISTS idx_parts_tenant_active
    ON parts(tenant_id, is_active);

CREATE TABLE IF NOT EXISTS part_stocks (
    id                 TEXT PRIMARY KEY,
    tenant_id          TEXT NOT NULL,
    part_id            TEXT NOT NULL,
    facility_id        TEXT NOT NULL,
    quantity_on_hand   REAL NOT NULL DEFAULT 0.0 CHECK (quantity_on_hand >= 0.0),
    quantity_reserved  REAL NOT NULL DEFAULT 0.0 CHECK (quantity_reserved >= 0.0),
    bin_location       TEXT NOT NULL DEFAULT '',
    updated_at         DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    FOREIGN KEY (part_id) REFERENCES parts(id) ON DELETE CASCADE,
    FOREIGN KEY (facility_id) REFERENCES facilities(id) ON DELETE CASCADE,
    UNIQUE (tenant_id, part_id, facility_id)
);

CREATE INDEX IF NOT EXISTS idx_part_stocks_tenant_facility
    ON part_stocks(tenant_id, facility_id);

CREATE INDEX IF NOT EXISTS idx_part_stocks_tenant_part
    ON part_stocks(tenant_id, part_id);

CREATE TABLE IF NOT EXISTS part_purchase_bills (
    id             TEXT PRIMARY KEY,
    tenant_id      TEXT NOT NULL,
    bill_number    TEXT NOT NULL,
    vendor_name    TEXT NOT NULL,
    vendor_gstin   TEXT NOT NULL DEFAULT '',
    facility_id    TEXT NOT NULL,
    bill_date      DATE NOT NULL,
    total_amount   REAL NOT NULL DEFAULT 0.0 CHECK (total_amount >= 0.0),
    tax_amount     REAL NOT NULL DEFAULT 0.0 CHECK (tax_amount >= 0.0),
    status         TEXT NOT NULL DEFAULT 'DRAFT' CHECK (status IN ('DRAFT', 'RECEIVED', 'CANCELLED')),
    received_date  DATETIME,
    received_by    TEXT NOT NULL DEFAULT '',
    notes          TEXT NOT NULL DEFAULT '',
    created_at     DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at     DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    FOREIGN KEY (facility_id) REFERENCES facilities(id),
    UNIQUE (tenant_id, bill_number)
);

CREATE INDEX IF NOT EXISTS idx_part_bills_tenant_status
    ON part_purchase_bills(tenant_id, status);

CREATE INDEX IF NOT EXISTS idx_part_bills_tenant_facility
    ON part_purchase_bills(tenant_id, facility_id);

CREATE TABLE IF NOT EXISTS part_purchase_items (
    id             TEXT PRIMARY KEY,
    tenant_id      TEXT NOT NULL,
    bill_id        TEXT NOT NULL,
    part_id        TEXT NOT NULL,
    quantity       REAL NOT NULL CHECK (quantity > 0.0),
    unit_cost      REAL NOT NULL CHECK (unit_cost >= 0.0),
    tax_rate       REAL NOT NULL DEFAULT 0.0 CHECK (tax_rate >= 0.0),
    total_cost     REAL NOT NULL CHECK (total_cost >= 0.0),
    created_at     DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    FOREIGN KEY (bill_id) REFERENCES part_purchase_bills(id) ON DELETE CASCADE,
    FOREIGN KEY (part_id) REFERENCES parts(id)
);

CREATE INDEX IF NOT EXISTS idx_part_items_bill
    ON part_purchase_items(bill_id);

CREATE INDEX IF NOT EXISTS idx_part_items_part
    ON part_purchase_items(part_id);

CREATE TABLE IF NOT EXISTS part_transactions (
    id               TEXT PRIMARY KEY,
    tenant_id        TEXT NOT NULL,
    part_id          TEXT NOT NULL,
    facility_id      TEXT NOT NULL,
    transaction_type TEXT NOT NULL CHECK (transaction_type IN ('PURCHASE_RECEIPT', 'WORK_ORDER_ISSUE', 'WORK_ORDER_RETURN', 'STO_OUT', 'STO_IN', 'ADJUSTMENT_IN', 'ADJUSTMENT_OUT', 'SCRAP')),
    quantity         REAL NOT NULL CHECK (quantity > 0.0),
    unit_cost        REAL NOT NULL DEFAULT 0.0 CHECK (unit_cost >= 0.0),
    total_cost       REAL NOT NULL DEFAULT 0.0 CHECK (total_cost >= 0.0),
    balance_after    REAL NOT NULL DEFAULT 0.0 CHECK (balance_after >= 0.0),
    reference_type   TEXT NOT NULL DEFAULT '',
    reference_id     TEXT NOT NULL DEFAULT '',
    vehicle_id       TEXT,
    work_order_id    TEXT,
    notes            TEXT NOT NULL DEFAULT '',
    performed_by     TEXT NOT NULL DEFAULT '',
    recorded_at      DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    FOREIGN KEY (part_id) REFERENCES parts(id),
    FOREIGN KEY (facility_id) REFERENCES facilities(id),
    FOREIGN KEY (vehicle_id) REFERENCES vehicles(id),
    FOREIGN KEY (work_order_id) REFERENCES work_orders(id)
);

CREATE INDEX IF NOT EXISTS idx_part_tx_part
    ON part_transactions(tenant_id, part_id, recorded_at DESC);

CREATE INDEX IF NOT EXISTS idx_part_tx_facility
    ON part_transactions(tenant_id, facility_id, recorded_at DESC);

CREATE INDEX IF NOT EXISTS idx_part_tx_work_order
    ON part_transactions(tenant_id, work_order_id);

-- Tenant isolation triggers
-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_parts_tenant_fk_insert
BEFORE INSERT ON parts
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for parts.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_parts_tenant_fk_update
BEFORE UPDATE OF tenant_id ON parts
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for parts.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_part_stocks_tenant_fk_insert
BEFORE INSERT ON part_stocks
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for part_stocks.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_part_stocks_tenant_fk_update
BEFORE UPDATE OF tenant_id ON part_stocks
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for part_stocks.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_part_purchase_bills_tenant_fk_insert
BEFORE INSERT ON part_purchase_bills
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for part_purchase_bills.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_part_purchase_bills_tenant_fk_update
BEFORE UPDATE OF tenant_id ON part_purchase_bills
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for part_purchase_bills.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_part_transactions_tenant_fk_insert
BEFORE INSERT ON part_transactions
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for part_transactions.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_part_transactions_tenant_fk_update
BEFORE UPDATE OF tenant_id ON part_transactions
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for part_transactions.tenant_id') END;
END;
-- +goose StatementEnd

-- RBAC Seeding & Grants
INSERT OR IGNORE INTO permissions (name, description) VALUES
('inventory:read', 'View spare parts catalog, multi-store stock levels, and transaction ledger'),
('inventory:write', 'Manage spare parts, receive purchase bills, issue parts to work orders, and adjust stock');

-- Grant to Role 1 (Admin)
INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT 1, id FROM permissions WHERE name IN ('inventory:read', 'inventory:write');

-- Grant to Role 2 (Dispatcher/Workshop)
INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT 2, id FROM permissions WHERE name IN ('inventory:read', 'inventory:write');

-- Grant to Role 6 (Org Admin)
INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT 6, id FROM permissions WHERE name IN ('inventory:read', 'inventory:write');

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN (SELECT id FROM permissions WHERE name IN ('inventory:read', 'inventory:write'));
DELETE FROM permissions WHERE name IN ('inventory:read', 'inventory:write');

DROP TRIGGER IF EXISTS trg_part_transactions_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_part_transactions_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_part_purchase_bills_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_part_purchase_bills_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_part_stocks_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_part_stocks_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_parts_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_parts_tenant_fk_insert;

DROP INDEX IF EXISTS idx_part_tx_work_order;
DROP INDEX IF EXISTS idx_part_tx_facility;
DROP INDEX IF EXISTS idx_part_tx_part;
DROP TABLE IF EXISTS part_transactions;

DROP INDEX IF EXISTS idx_part_items_part;
DROP INDEX IF EXISTS idx_part_items_bill;
DROP TABLE IF EXISTS part_purchase_items;

DROP INDEX IF EXISTS idx_part_bills_tenant_facility;
DROP INDEX IF EXISTS idx_part_bills_tenant_status;
DROP TABLE IF EXISTS part_purchase_bills;

DROP INDEX IF EXISTS idx_part_stocks_tenant_part;
DROP INDEX IF EXISTS idx_part_stocks_tenant_facility;
DROP TABLE IF EXISTS part_stocks;

DROP INDEX IF EXISTS idx_parts_tenant_active;
DROP INDEX IF EXISTS idx_parts_tenant_category;
DROP INDEX IF EXISTS idx_parts_tenant_number;
DROP TABLE IF EXISTS parts;
