-- PG port of 00146_sto_and_load_board.sql | status: PORTABLE | flags: none
-- +goose Up

CREATE TABLE IF NOT EXISTS stock_transfer_orders (
    id                      TEXT PRIMARY KEY,
    tenant_id               TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    sto_number              TEXT NOT NULL,
    origin_facility_id      TEXT NOT NULL REFERENCES facilities(id),
    destination_facility_id TEXT NOT NULL REFERENCES facilities(id),
    material_code           TEXT NOT NULL,
    material_description    TEXT NOT NULL,
    quantity                DOUBLE PRECISION NOT NULL CHECK (quantity > 0),
    uom                     TEXT NOT NULL,
    required_delivery_date  TEXT NOT NULL,
    status                  TEXT NOT NULL DEFAULT 'DRAFT'
                            CHECK (status IN ('DRAFT', 'RELEASED', 'POSTED', 'ASSIGNED', 'IN_TRANSIT', 'RECEIVED', 'CANCELLED')),
    notes                   TEXT,
    created_by              TEXT NOT NULL,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_sto_tenant_number UNIQUE (tenant_id, sto_number)
);

CREATE INDEX IF NOT EXISTS idx_sto_tenant_status
    ON stock_transfer_orders(tenant_id, status, required_delivery_date);

CREATE TABLE IF NOT EXISTS load_board_listings (
    id                      TEXT PRIMARY KEY,
    tenant_id               TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    sto_id                  TEXT REFERENCES stock_transfer_orders(id) ON DELETE SET NULL,
    booking_id              TEXT REFERENCES bookings(id) ON DELETE SET NULL,
    origin_city             TEXT NOT NULL,
    destination_city        TEXT NOT NULL,
    vehicle_type_required   TEXT NOT NULL,
    target_rate             DOUBLE PRECISION NOT NULL CHECK (target_rate >= 0),
    max_rate                DOUBLE PRECISION NOT NULL CHECK (max_rate >= target_rate),
    visibility              TEXT NOT NULL DEFAULT 'PRIVATE'
                            CHECK (visibility IN ('PRIVATE', 'FEDERATED', 'PUBLIC')),
    status                  TEXT NOT NULL DEFAULT 'OPEN'
                            CHECK (status IN ('OPEN', 'BIDDING', 'AWARDED', 'EXPIRED', 'CANCELLED')),
    expires_at              TIMESTAMPTZ NOT NULL,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT chk_loadboard_source CHECK (sto_id IS NOT NULL OR booking_id IS NOT NULL)
);

CREATE INDEX IF NOT EXISTS idx_loadboard_tenant_status
    ON load_board_listings(tenant_id, status, expires_at);

CREATE TABLE IF NOT EXISTS load_board_bids (
    id                      TEXT PRIMARY KEY,
    tenant_id               TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    listing_id              TEXT NOT NULL REFERENCES load_board_listings(id) ON DELETE CASCADE,
    carrier_id              TEXT NOT NULL,
    carrier_name            TEXT NOT NULL,
    bid_amount              DOUBLE PRECISION NOT NULL CHECK (bid_amount > 0),
    vehicle_id              TEXT,
    driver_id               TEXT,
    status                  TEXT NOT NULL DEFAULT 'SUBMITTED'
                            CHECK (status IN ('SUBMITTED', 'ACCEPTED', 'REJECTED', 'WITHDRAWN')),
    remarks                 TEXT,
    submitted_at            TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    decided_at              TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_loadboard_bids_listing
    ON load_board_bids(tenant_id, listing_id, status);

DROP TRIGGER IF EXISTS trg_sto_tenant_fk_insert ON stock_transfer_orders;
CREATE TRIGGER trg_sto_tenant_fk_insert BEFORE INSERT ON stock_transfer_orders
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();

DROP TRIGGER IF EXISTS trg_sto_tenant_fk_update ON stock_transfer_orders;
CREATE TRIGGER trg_sto_tenant_fk_update BEFORE UPDATE OF tenant_id ON stock_transfer_orders
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();

DROP TRIGGER IF EXISTS trg_loadboard_tenant_fk_insert ON load_board_listings;
CREATE TRIGGER trg_loadboard_tenant_fk_insert BEFORE INSERT ON load_board_listings
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();

DROP TRIGGER IF EXISTS trg_loadboard_tenant_fk_update ON load_board_listings;
CREATE TRIGGER trg_loadboard_tenant_fk_update BEFORE UPDATE OF tenant_id ON load_board_listings
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();

DROP TRIGGER IF EXISTS trg_loadboard_bids_tenant_fk_insert ON load_board_bids;
CREATE TRIGGER trg_loadboard_bids_tenant_fk_insert BEFORE INSERT ON load_board_bids
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();

DROP TRIGGER IF EXISTS trg_loadboard_bids_tenant_fk_update ON load_board_bids;
CREATE TRIGGER trg_loadboard_bids_tenant_fk_update BEFORE UPDATE OF tenant_id ON load_board_bids
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();

INSERT INTO permissions (name, description) VALUES
('sto:read', 'View stock transfer orders and status'),
('sto:write', 'Create, release, and manage stock transfer orders'),
('loadboard:read', 'View load board listings and bids'),
('loadboard:write', 'Post listings, submit bids, and award loads')
ON CONFLICT (name) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT 1, id FROM permissions WHERE name IN ('sto:read', 'sto:write', 'loadboard:read', 'loadboard:write')
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT 6, id FROM permissions WHERE name IN ('sto:read', 'sto:write', 'loadboard:read', 'loadboard:write')
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT 2, id FROM permissions WHERE name IN ('sto:read', 'sto:write', 'loadboard:read', 'loadboard:write')
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN (
    SELECT id FROM permissions WHERE name IN ('sto:read', 'sto:write', 'loadboard:read', 'loadboard:write')
);
DELETE FROM permissions WHERE name IN ('sto:read', 'sto:write', 'loadboard:read', 'loadboard:write');

DROP TRIGGER IF EXISTS trg_loadboard_bids_tenant_fk_update ON load_board_bids;
DROP TRIGGER IF EXISTS trg_loadboard_bids_tenant_fk_insert ON load_board_bids;
DROP TRIGGER IF EXISTS trg_loadboard_tenant_fk_update ON load_board_listings;
DROP TRIGGER IF EXISTS trg_loadboard_tenant_fk_insert ON load_board_listings;
DROP TRIGGER IF EXISTS trg_sto_tenant_fk_update ON stock_transfer_orders;
DROP TRIGGER IF EXISTS trg_sto_tenant_fk_insert ON stock_transfer_orders;

DROP INDEX IF EXISTS idx_loadboard_bids_listing;
DROP TABLE IF EXISTS load_board_bids CASCADE;

DROP INDEX IF EXISTS idx_loadboard_tenant_status;
DROP TABLE IF EXISTS load_board_listings CASCADE;

DROP INDEX IF EXISTS idx_sto_tenant_status;
DROP TABLE IF EXISTS stock_transfer_orders CASCADE;
