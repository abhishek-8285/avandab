-- +goose Up
-- PG port of 00173_freight_audit.sql
-- Freight Audit & Pay Engine (FleetX Parity P3.3)
-- Automated 3-way matching and variance audit: Billed Rate/Distance/Weight/Tolls
-- vs Contract Rate Card, GPS/Odometer Distance, Consignment Weight, and FASTag Electronic Tolls.

CREATE TABLE IF NOT EXISTS freight_rate_cards (
    id                      TEXT PRIMARY KEY,
    tenant_id               TEXT NOT NULL REFERENCES tenants(id),
    contract_number         TEXT NOT NULL,
    name                    TEXT NOT NULL,
    vendor_id               TEXT,
    customer_id             TEXT,
    route_id                TEXT REFERENCES routes(id),
    vehicle_type            TEXT NOT NULL DEFAULT '',
    billing_basis           TEXT NOT NULL CHECK (billing_basis IN ('PER_KM', 'PER_KG', 'PER_TON', 'PER_TON_KM', 'FLAT')),
    rate                    DOUBLE PRECISION NOT NULL CHECK (rate >= 0.0),
    min_charge              DOUBLE PRECISION NOT NULL DEFAULT 0.0 CHECK (min_charge >= 0.0),
    free_detention_hours    DOUBLE PRECISION NOT NULL DEFAULT 0.0 CHECK (free_detention_hours >= 0.0),
    detention_rate_per_hour DOUBLE PRECISION NOT NULL DEFAULT 0.0 CHECK (detention_rate_per_hour >= 0.0),
    effective_from          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    effective_to            TIMESTAMPTZ,
    is_active               BOOLEAN NOT NULL DEFAULT TRUE,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_freight_rates_tenant_contract
    ON freight_rate_cards(tenant_id, contract_number);

CREATE INDEX IF NOT EXISTS idx_freight_rates_lookup
    ON freight_rate_cards(tenant_id, route_id, vehicle_type, is_active);

CREATE TABLE IF NOT EXISTS freight_audits (
    id                       TEXT PRIMARY KEY,
    tenant_id                TEXT NOT NULL REFERENCES tenants(id),
    audit_number             TEXT NOT NULL UNIQUE,
    trip_id                  TEXT REFERENCES trips(id),
    invoice_id               TEXT REFERENCES invoices(id),
    booking_id               TEXT REFERENCES bookings(id),
    rate_card_id             TEXT REFERENCES freight_rate_cards(id),
    vendor_name              TEXT NOT NULL DEFAULT '',
    vehicle_number           TEXT NOT NULL DEFAULT '',
    status                   TEXT NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING', 'VERIFIED', 'DISCREPANCY_FLAGGED', 'DEDUCTION_APPROVED', 'DISPUTED', 'SETTLED')),
    billing_basis            TEXT NOT NULL CHECK (billing_basis IN ('PER_KM', 'PER_KG', 'PER_TON', 'PER_TON_KM', 'FLAT')),

    -- Billed amounts & claimed metrics
    billed_distance_km       DOUBLE PRECISION NOT NULL DEFAULT 0.0 CHECK (billed_distance_km >= 0.0),
    billed_weight_kg         DOUBLE PRECISION NOT NULL DEFAULT 0.0 CHECK (billed_weight_kg >= 0.0),
    billed_rate              DOUBLE PRECISION NOT NULL DEFAULT 0.0 CHECK (billed_rate >= 0.0),
    billed_base_freight      DOUBLE PRECISION NOT NULL DEFAULT 0.0 CHECK (billed_base_freight >= 0.0),
    billed_toll_charges      DOUBLE PRECISION NOT NULL DEFAULT 0.0 CHECK (billed_toll_charges >= 0.0),
    billed_detention_charges DOUBLE PRECISION NOT NULL DEFAULT 0.0 CHECK (billed_detention_charges >= 0.0),
    billed_total             DOUBLE PRECISION NOT NULL DEFAULT 0.0 CHECK (billed_total >= 0.0),

    -- Audited ground-truth metrics
    gps_distance_km          DOUBLE PRECISION NOT NULL DEFAULT 0.0 CHECK (gps_distance_km >= 0.0),
    actual_weight_kg         DOUBLE PRECISION NOT NULL DEFAULT 0.0 CHECK (actual_weight_kg >= 0.0),
    audited_rate             DOUBLE PRECISION NOT NULL DEFAULT 0.0 CHECK (audited_rate >= 0.0),
    audited_base_freight     DOUBLE PRECISION NOT NULL DEFAULT 0.0 CHECK (audited_base_freight >= 0.0),
    audited_toll_charges     DOUBLE PRECISION NOT NULL DEFAULT 0.0 CHECK (audited_toll_charges >= 0.0),
    audited_detention_hours  DOUBLE PRECISION NOT NULL DEFAULT 0.0 CHECK (audited_detention_hours >= 0.0),
    audited_detention_charges DOUBLE PRECISION NOT NULL DEFAULT 0.0 CHECK (audited_detention_charges >= 0.0),
    audited_total            DOUBLE PRECISION NOT NULL DEFAULT 0.0 CHECK (audited_total >= 0.0),

    -- Variances & overbilling calculation
    distance_variance_km     DOUBLE PRECISION NOT NULL DEFAULT 0.0,
    weight_variance_kg       DOUBLE PRECISION NOT NULL DEFAULT 0.0,
    rate_variance            DOUBLE PRECISION NOT NULL DEFAULT 0.0,
    toll_variance            DOUBLE PRECISION NOT NULL DEFAULT 0.0,
    overbilling_amount       DOUBLE PRECISION NOT NULL DEFAULT 0.0,
    variance_pct             DOUBLE PRECISION NOT NULL DEFAULT 0.0,

    -- Deductions & settlement
    deduction_amount         DOUBLE PRECISION NOT NULL DEFAULT 0.0 CHECK (deduction_amount >= 0.0),
    payable_amount           DOUBLE PRECISION NOT NULL DEFAULT 0.0 CHECK (payable_amount >= 0.0),
    deduction_reason         TEXT NOT NULL DEFAULT '',
    debit_note_number        TEXT NOT NULL DEFAULT '',

    -- Audit trail & resolution
    discrepancy_flags        TEXT NOT NULL DEFAULT '',
    dispute_reason           TEXT NOT NULL DEFAULT '',
    reviewed_by              TEXT NOT NULL DEFAULT '',
    reviewed_at              TIMESTAMPTZ,
    approved_by              TEXT NOT NULL DEFAULT '',
    approved_at              TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_freight_audits_tenant_status
    ON freight_audits(tenant_id, status);

CREATE INDEX IF NOT EXISTS idx_freight_audits_tenant_trip
    ON freight_audits(tenant_id, trip_id);

CREATE INDEX IF NOT EXISTS idx_freight_audits_tenant_invoice
    ON freight_audits(tenant_id, invoice_id);

CREATE INDEX IF NOT EXISTS idx_freight_audits_created
    ON freight_audits(tenant_id, created_at DESC);

CREATE TABLE IF NOT EXISTS freight_audit_deductions (
    id               TEXT PRIMARY KEY,
    tenant_id        TEXT NOT NULL REFERENCES tenants(id),
    audit_id         TEXT NOT NULL REFERENCES freight_audits(id) ON DELETE CASCADE,
    category         TEXT NOT NULL CHECK (category IN ('DISTANCE_INFLATION', 'WEIGHT_MISMATCH', 'RATE_OVERCHARGE', 'UNVERIFIED_TOLL', 'EXCESS_DETENTION', 'DAMAGE_PENALTY', 'OTHER')),
    description      TEXT NOT NULL,
    claimed_value    DOUBLE PRECISION NOT NULL DEFAULT 0.0,
    verified_value   DOUBLE PRECISION NOT NULL DEFAULT 0.0,
    unit_rate        DOUBLE PRECISION NOT NULL DEFAULT 0.0,
    deduction_amount DOUBLE PRECISION NOT NULL CHECK (deduction_amount >= 0.0),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_freight_deductions_audit
    ON freight_audit_deductions(tenant_id, audit_id);

-- RBAC Seeding & Grants
INSERT INTO permissions (name, description) VALUES
('freight_audit:read', 'View freight audit reports, rate cards, and billing variance calculations'),
('freight_audit:write', 'Run freight audits, approve overbilling deductions, manage rate cards, and issue debit notes')
ON CONFLICT (name) DO NOTHING;

-- Grant to Role 1 (Admin)
INSERT INTO role_permissions (role_id, permission_id)
SELECT 1, id FROM permissions WHERE name IN ('freight_audit:read', 'freight_audit:write')
ON CONFLICT (role_id, permission_id) DO NOTHING;

-- Grant to Role 2 (Dispatcher / Ops)
INSERT INTO role_permissions (role_id, permission_id)
SELECT 2, id FROM permissions WHERE name IN ('freight_audit:read', 'freight_audit:write')
ON CONFLICT (role_id, permission_id) DO NOTHING;

-- Grant to Role 6 (Org Admin)
INSERT INTO role_permissions (role_id, permission_id)
SELECT 6, id FROM permissions WHERE name IN ('freight_audit:read', 'freight_audit:write')
ON CONFLICT (role_id, permission_id) DO NOTHING;

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN (SELECT id FROM permissions WHERE name IN ('freight_audit:read', 'freight_audit:write'));
DELETE FROM permissions WHERE name IN ('freight_audit:read', 'freight_audit:write');

DROP INDEX IF EXISTS idx_freight_deductions_audit;
DROP TABLE IF EXISTS freight_audit_deductions;

DROP INDEX IF EXISTS idx_freight_audits_created;
DROP INDEX IF EXISTS idx_freight_audits_tenant_invoice;
DROP INDEX IF EXISTS idx_freight_audits_tenant_trip;
DROP INDEX IF EXISTS idx_freight_audits_tenant_status;
DROP TABLE IF EXISTS freight_audits;

DROP INDEX IF EXISTS idx_freight_rates_lookup;
DROP INDEX IF EXISTS idx_freight_rates_tenant_contract;
DROP TABLE IF EXISTS freight_rate_cards;
