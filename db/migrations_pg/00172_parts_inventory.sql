-- PG port of 00172_parts_inventory.sql | status: PORTABLE | flags: none
-- +goose Up
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
    min_stock_level    DOUBLE PRECISION NOT NULL DEFAULT 5.0 CHECK (min_stock_level >= 0.0),
    reorder_quantity   DOUBLE PRECISION NOT NULL DEFAULT 10.0 CHECK (reorder_quantity >= 0.0),
    cost_price         DOUBLE PRECISION NOT NULL DEFAULT 0.0 CHECK (cost_price >= 0.0),
    selling_price      DOUBLE PRECISION NOT NULL DEFAULT 0.0 CHECK (selling_price >= 0.0),
    oem_brand          TEXT NOT NULL DEFAULT '',
    compatible_models  TEXT NOT NULL DEFAULT '',
    is_active          INTEGER NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
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
    quantity_on_hand   DOUBLE PRECISION NOT NULL DEFAULT 0.0 CHECK (quantity_on_hand >= 0.0),
    quantity_reserved  DOUBLE PRECISION NOT NULL DEFAULT 0.0 CHECK (quantity_reserved >= 0.0),
    bin_location       TEXT NOT NULL DEFAULT '',
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
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
    total_amount   DOUBLE PRECISION NOT NULL DEFAULT 0.0 CHECK (total_amount >= 0.0),
    tax_amount     DOUBLE PRECISION NOT NULL DEFAULT 0.0 CHECK (tax_amount >= 0.0),
    status         TEXT NOT NULL DEFAULT 'DRAFT' CHECK (status IN ('DRAFT', 'RECEIVED', 'CANCELLED')),
    received_date  TIMESTAMPTZ,
    received_by    TEXT NOT NULL DEFAULT '',
    notes          TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
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
    quantity       DOUBLE PRECISION NOT NULL CHECK (quantity > 0.0),
    unit_cost      DOUBLE PRECISION NOT NULL CHECK (unit_cost >= 0.0),
    tax_rate       DOUBLE PRECISION NOT NULL DEFAULT 0.0 CHECK (tax_rate >= 0.0),
    total_cost     DOUBLE PRECISION NOT NULL CHECK (total_cost >= 0.0),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
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
    quantity         DOUBLE PRECISION NOT NULL CHECK (quantity > 0.0),
    unit_cost        DOUBLE PRECISION NOT NULL DEFAULT 0.0 CHECK (unit_cost >= 0.0),
    total_cost       DOUBLE PRECISION NOT NULL DEFAULT 0.0 CHECK (total_cost >= 0.0),
    balance_after    DOUBLE PRECISION NOT NULL DEFAULT 0.0 CHECK (balance_after >= 0.0),
    reference_type   TEXT NOT NULL DEFAULT '',
    reference_id     TEXT NOT NULL DEFAULT '',
    vehicle_id       TEXT,
    work_order_id    TEXT,
    notes            TEXT NOT NULL DEFAULT '',
    performed_by     TEXT NOT NULL DEFAULT '',
    recorded_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
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

-- RBAC Seeding & Grants
INSERT INTO permissions (name, description) VALUES
('inventory:read', 'View spare parts catalog, multi-store stock levels, and transaction ledger'),
('inventory:write', 'Manage spare parts, receive purchase bills, issue parts to work orders, and adjust stock')
ON CONFLICT (name) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT 1, id FROM permissions WHERE name IN ('inventory:read', 'inventory:write')
ON CONFLICT (role_id, permission_id) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT 2, id FROM permissions WHERE name IN ('inventory:read', 'inventory:write')
ON CONFLICT (role_id, permission_id) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT 6, id FROM permissions WHERE name IN ('inventory:read', 'inventory:write')
ON CONFLICT (role_id, permission_id) DO NOTHING;

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN (SELECT id FROM permissions WHERE name IN ('inventory:read', 'inventory:write'));
DELETE FROM permissions WHERE name IN ('inventory:read', 'inventory:write');

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
