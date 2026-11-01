-- +goose Up
-- PG port of 00175_rfq_and_counter_offer.sql
-- RFQ & Counter-Offer Negotiation Engine (FleetX Parity P3.5)
-- Multi-round lane bidding, Request For Quotation (RFQ) lifecycle,
-- counter-offer negotiations on top of Load Board & spot auctions.

CREATE TABLE IF NOT EXISTS rfqs (
    id                    TEXT PRIMARY KEY,
    tenant_id             TEXT NOT NULL REFERENCES tenants(id),
    rfq_number            TEXT NOT NULL,
    title                 TEXT NOT NULL,
    origin_city           TEXT NOT NULL,
    destination_city      TEXT NOT NULL,
    vehicle_type_required TEXT NOT NULL,
    cargo_type            TEXT NOT NULL DEFAULT '',
    cargo_weight_tons     REAL NOT NULL DEFAULT 0.0 CHECK (cargo_weight_tons >= 0.0),
    target_rate           REAL NOT NULL DEFAULT 0.0 CHECK (target_rate >= 0.0),
    max_acceptable_rate   REAL NOT NULL DEFAULT 0.0 CHECK (max_acceptable_rate >= target_rate),
    pickup_date           TIMESTAMPTZ NOT NULL,
    delivery_deadline     TIMESTAMPTZ NOT NULL,
    bid_deadline          TIMESTAMPTZ NOT NULL,
    status                TEXT NOT NULL DEFAULT 'OPEN'
                          CHECK (status IN ('OPEN', 'UNDER_NEGOTIATION', 'AWARDED', 'CLOSED', 'EXPIRED', 'CANCELLED')),
    booking_id            TEXT REFERENCES bookings(id) ON DELETE SET NULL,
    sto_id                TEXT REFERENCES stock_transfer_orders(id) ON DELETE SET NULL,
    awarded_bid_id        TEXT,
    awarded_carrier_id    TEXT NOT NULL DEFAULT '',
    awarded_carrier_name  TEXT NOT NULL DEFAULT '',
    awarded_rate          REAL NOT NULL DEFAULT 0.0,
    awarded_at            TIMESTAMPTZ,
    created_by            TEXT NOT NULL DEFAULT '',
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, rfq_number)
);

CREATE INDEX IF NOT EXISTS idx_rfqs_tenant_status
    ON rfqs(tenant_id, status, bid_deadline);

CREATE INDEX IF NOT EXISTS idx_rfqs_tenant_lane
    ON rfqs(tenant_id, origin_city, destination_city);

CREATE TABLE IF NOT EXISTS rfq_bids (
    id                 TEXT PRIMARY KEY,
    tenant_id          TEXT NOT NULL REFERENCES tenants(id),
    rfq_id             TEXT NOT NULL REFERENCES rfqs(id) ON DELETE CASCADE,
    carrier_id         TEXT NOT NULL,
    carrier_name       TEXT NOT NULL,
    bid_amount         REAL NOT NULL CHECK (bid_amount > 0.0),
    transit_days       INTEGER NOT NULL DEFAULT 1 CHECK (transit_days > 0),
    vehicles_available INTEGER NOT NULL DEFAULT 1 CHECK (vehicles_available > 0),
    remarks            TEXT NOT NULL DEFAULT '',
    status             TEXT NOT NULL DEFAULT 'SUBMITTED'
                       CHECK (status IN ('SUBMITTED', 'COUNTER_OFFERED', 'ACCEPTED', 'REJECTED', 'WITHDRAWN')),
    submitted_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    decided_at         TIMESTAMPTZ,
    decided_by         TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_rfq_bids_rfq
    ON rfq_bids(tenant_id, rfq_id, status);

CREATE TABLE IF NOT EXISTS counter_offers (
    id             TEXT PRIMARY KEY,
    tenant_id      TEXT NOT NULL REFERENCES tenants(id),
    entity_type    TEXT NOT NULL CHECK (entity_type IN ('RFQ', 'LOAD_BOARD')),
    rfq_id         TEXT REFERENCES rfqs(id) ON DELETE CASCADE,
    listing_id     TEXT REFERENCES load_board_listings(id) ON DELETE CASCADE,
    bid_id         TEXT NOT NULL,
    round_number   INTEGER NOT NULL DEFAULT 1 CHECK (round_number >= 1),
    sender_role    TEXT NOT NULL CHECK (sender_role IN ('SHIPPER', 'CARRIER')),
    sender_id      TEXT NOT NULL DEFAULT '',
    sender_name    TEXT NOT NULL DEFAULT '',
    offered_amount REAL NOT NULL CHECK (offered_amount > 0.0),
    remarks        TEXT NOT NULL DEFAULT '',
    status         TEXT NOT NULL DEFAULT 'PENDING'
                   CHECK (status IN ('PENDING', 'ACCEPTED', 'REJECTED', 'SUPERSEDED', 'EXPIRED')),
    expires_at     TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    decided_at     TIMESTAMPTZ,
    decided_by     TEXT NOT NULL DEFAULT '',
    CHECK (rfq_id IS NOT NULL OR listing_id IS NOT NULL)
);

CREATE INDEX IF NOT EXISTS idx_counter_offers_bid
    ON counter_offers(tenant_id, bid_id, status);

CREATE INDEX IF NOT EXISTS idx_counter_offers_rfq
    ON counter_offers(tenant_id, rfq_id);

CREATE INDEX IF NOT EXISTS idx_counter_offers_listing
    ON counter_offers(tenant_id, listing_id);

-- RBAC Seeding & Grants
INSERT INTO permissions (name, description) VALUES
('rfq:read', 'View RFQs, carrier quotes, and counter-offer negotiation rounds'),
('rfq:write', 'Create RFQs, submit quotes, negotiate counter-offers, and award contracts')
ON CONFLICT (name) DO NOTHING;

-- Grant to Role 1 (Admin)
INSERT INTO role_permissions (role_id, permission_id)
SELECT 1, id FROM permissions WHERE name IN ('rfq:read', 'rfq:write')
ON CONFLICT (role_id, permission_id) DO NOTHING;

-- Grant to Role 2 (Dispatcher / Ops)
INSERT INTO role_permissions (role_id, permission_id)
SELECT 2, id FROM permissions WHERE name IN ('rfq:read', 'rfq:write')
ON CONFLICT (role_id, permission_id) DO NOTHING;

-- Grant to Role 6 (Org Admin)
INSERT INTO role_permissions (role_id, permission_id)
SELECT 6, id FROM permissions WHERE name IN ('rfq:read', 'rfq:write')
ON CONFLICT (role_id, permission_id) DO NOTHING;

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN (SELECT id FROM permissions WHERE name IN ('rfq:read', 'rfq:write'));
DELETE FROM permissions WHERE name IN ('rfq:read', 'rfq:write');

DROP INDEX IF EXISTS idx_counter_offers_listing;
DROP INDEX IF EXISTS idx_counter_offers_rfq;
DROP INDEX IF EXISTS idx_counter_offers_bid;
DROP TABLE IF EXISTS counter_offers;

DROP INDEX IF EXISTS idx_rfq_bids_rfq;
DROP TABLE IF EXISTS rfq_bids;

DROP INDEX IF EXISTS idx_rfqs_tenant_lane;
DROP INDEX IF EXISTS idx_rfqs_tenant_status;
DROP TABLE IF EXISTS rfqs;
