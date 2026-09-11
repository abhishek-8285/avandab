-- +goose Up
-- 00146: sto_and_load_board — Stock Transfer Order (STO) Portal & Load Board Listings (Spec 19 §2, B8).

CREATE TABLE IF NOT EXISTS stock_transfer_orders (
    id                      TEXT PRIMARY KEY,
    tenant_id               TEXT NOT NULL,
    sto_number              TEXT NOT NULL,
    origin_facility_id      TEXT NOT NULL,
    destination_facility_id TEXT NOT NULL,
    material_code           TEXT NOT NULL,
    material_description    TEXT NOT NULL,
    quantity                REAL NOT NULL CHECK (quantity > 0),
    uom                     TEXT NOT NULL,
    required_delivery_date  TEXT NOT NULL,
    status                  TEXT NOT NULL DEFAULT 'DRAFT'
                            CHECK (status IN ('DRAFT', 'RELEASED', 'POSTED', 'ASSIGNED', 'IN_TRANSIT', 'RECEIVED', 'CANCELLED')),
    notes                   TEXT,
    created_by              TEXT NOT NULL,
    created_at              DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at              DATETIME NOT NULL DEFAULT (datetime('now')),
    UNIQUE (tenant_id, sto_number),
    FOREIGN KEY (origin_facility_id) REFERENCES facilities(id),
    FOREIGN KEY (destination_facility_id) REFERENCES facilities(id)
);

CREATE INDEX IF NOT EXISTS idx_sto_tenant_status
    ON stock_transfer_orders(tenant_id, status, required_delivery_date);

CREATE TABLE IF NOT EXISTS load_board_listings (
    id                      TEXT PRIMARY KEY,
    tenant_id               TEXT NOT NULL,
    sto_id                  TEXT REFERENCES stock_transfer_orders(id) ON DELETE SET NULL,
    booking_id              TEXT REFERENCES bookings(id) ON DELETE SET NULL,
    origin_city             TEXT NOT NULL,
    destination_city        TEXT NOT NULL,
    vehicle_type_required   TEXT NOT NULL,
    target_rate             REAL NOT NULL CHECK (target_rate >= 0),
    max_rate                REAL NOT NULL CHECK (max_rate >= target_rate),
    visibility              TEXT NOT NULL DEFAULT 'PRIVATE'
                            CHECK (visibility IN ('PRIVATE', 'FEDERATED', 'PUBLIC')),
    status                  TEXT NOT NULL DEFAULT 'OPEN'
                            CHECK (status IN ('OPEN', 'BIDDING', 'AWARDED', 'EXPIRED', 'CANCELLED')),
    expires_at              DATETIME NOT NULL,
    created_at              DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at              DATETIME NOT NULL DEFAULT (datetime('now')),
    CHECK (sto_id IS NOT NULL OR booking_id IS NOT NULL)
);

CREATE INDEX IF NOT EXISTS idx_loadboard_tenant_status
    ON load_board_listings(tenant_id, status, expires_at);

CREATE TABLE IF NOT EXISTS load_board_bids (
    id                      TEXT PRIMARY KEY,
    tenant_id               TEXT NOT NULL,
    listing_id              TEXT NOT NULL REFERENCES load_board_listings(id) ON DELETE CASCADE,
    carrier_id              TEXT NOT NULL,
    carrier_name            TEXT NOT NULL,
    bid_amount              REAL NOT NULL CHECK (bid_amount > 0),
    vehicle_id              TEXT,
    driver_id               TEXT,
    status                  TEXT NOT NULL DEFAULT 'SUBMITTED'
                            CHECK (status IN ('SUBMITTED', 'ACCEPTED', 'REJECTED', 'WITHDRAWN')),
    remarks                 TEXT,
    submitted_at            DATETIME NOT NULL DEFAULT (datetime('now')),
    decided_at              DATETIME
);

CREATE INDEX IF NOT EXISTS idx_loadboard_bids_listing
    ON load_board_bids(tenant_id, listing_id, status);

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_sto_tenant_fk_insert
BEFORE INSERT ON stock_transfer_orders
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for stock_transfer_orders.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_sto_tenant_fk_update
BEFORE UPDATE OF tenant_id ON stock_transfer_orders
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for stock_transfer_orders.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_loadboard_tenant_fk_insert
BEFORE INSERT ON load_board_listings
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for load_board_listings.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_loadboard_tenant_fk_update
BEFORE UPDATE OF tenant_id ON load_board_listings
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for load_board_listings.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_loadboard_bids_tenant_fk_insert
BEFORE INSERT ON load_board_bids
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for load_board_bids.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_loadboard_bids_tenant_fk_update
BEFORE UPDATE OF tenant_id ON load_board_bids
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for load_board_bids.tenant_id') END;
END;
-- +goose StatementEnd

INSERT OR IGNORE INTO permissions (name, description) VALUES
('sto:read', 'View stock transfer orders and status'),
('sto:write', 'Create, release, and manage stock transfer orders'),
('loadboard:read', 'View load board listings and bids'),
('loadboard:write', 'Post listings, submit bids, and award loads');

-- Admin (1) gets all
INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT 1, id FROM permissions WHERE name IN ('sto:read', 'sto:write', 'loadboard:read', 'loadboard:write');

-- Org Admin (6) gets all
INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT 6, id FROM permissions WHERE name IN ('sto:read', 'sto:write', 'loadboard:read', 'loadboard:write');

-- Dispatcher (2) gets all
INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT 2, id FROM permissions WHERE name IN ('sto:read', 'sto:write', 'loadboard:read', 'loadboard:write');

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN (
    SELECT id FROM permissions WHERE name IN ('sto:read', 'sto:write', 'loadboard:read', 'loadboard:write')
);
DELETE FROM permissions WHERE name IN ('sto:read', 'sto:write', 'loadboard:read', 'loadboard:write');

DROP TRIGGER IF EXISTS trg_loadboard_bids_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_loadboard_bids_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_loadboard_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_loadboard_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_sto_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_sto_tenant_fk_insert;

DROP INDEX IF EXISTS idx_loadboard_bids_listing;
DROP TABLE IF EXISTS load_board_bids;

DROP INDEX IF EXISTS idx_loadboard_tenant_status;
DROP TABLE IF EXISTS load_board_listings;

DROP INDEX IF EXISTS idx_sto_tenant_status;
DROP TABLE IF EXISTS stock_transfer_orders;
