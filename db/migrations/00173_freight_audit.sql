-- +goose Up
-- 00173: Freight Audit & Pay Engine (FleetX Parity P3.3)
-- Automated 3-way matching and variance audit: Billed Rate/Distance/Weight/Tolls
-- vs Contract Rate Card, GPS/Odometer Distance, Consignment Weight, and FASTag Electronic Tolls.
-- Generates overbilling deduction line items and debit notes with RBAC controls.

CREATE TABLE IF NOT EXISTS freight_rate_cards (
    id                      TEXT PRIMARY KEY,
    tenant_id               TEXT NOT NULL,
    contract_number         TEXT NOT NULL,
    name                    TEXT NOT NULL,
    vendor_id               TEXT,
    customer_id             TEXT,
    route_id                TEXT,
    vehicle_type            TEXT NOT NULL DEFAULT '',
    billing_basis           TEXT NOT NULL CHECK (billing_basis IN ('PER_KM', 'PER_KG', 'PER_TON', 'PER_TON_KM', 'FLAT')),
    rate                    REAL NOT NULL CHECK (rate >= 0.0),
    min_charge              REAL NOT NULL DEFAULT 0.0 CHECK (min_charge >= 0.0),
    free_detention_hours    REAL NOT NULL DEFAULT 0.0 CHECK (free_detention_hours >= 0.0),
    detention_rate_per_hour REAL NOT NULL DEFAULT 0.0 CHECK (detention_rate_per_hour >= 0.0),
    effective_from          DATETIME NOT NULL DEFAULT (datetime('now')),
    effective_to            DATETIME,
    is_active               INTEGER NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),
    created_at              DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at              DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    FOREIGN KEY (route_id) REFERENCES routes(id)
);

CREATE INDEX IF NOT EXISTS idx_freight_rates_tenant_contract
    ON freight_rate_cards(tenant_id, contract_number);

CREATE INDEX IF NOT EXISTS idx_freight_rates_lookup
    ON freight_rate_cards(tenant_id, route_id, vehicle_type, is_active);

CREATE TABLE IF NOT EXISTS freight_audits (
    id                       TEXT PRIMARY KEY,
    tenant_id                TEXT NOT NULL,
    audit_number             TEXT NOT NULL UNIQUE,
    trip_id                  TEXT,
    invoice_id               TEXT,
    booking_id               TEXT,
    rate_card_id             TEXT,
    vendor_name              TEXT NOT NULL DEFAULT '',
    vehicle_number           TEXT NOT NULL DEFAULT '',
    status                   TEXT NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING', 'VERIFIED', 'DISCREPANCY_FLAGGED', 'DEDUCTION_APPROVED', 'DISPUTED', 'SETTLED')),
    billing_basis            TEXT NOT NULL CHECK (billing_basis IN ('PER_KM', 'PER_KG', 'PER_TON', 'PER_TON_KM', 'FLAT')),

    -- Billed amounts & claimed metrics
    billed_distance_km       REAL NOT NULL DEFAULT 0.0 CHECK (billed_distance_km >= 0.0),
    billed_weight_kg         REAL NOT NULL DEFAULT 0.0 CHECK (billed_weight_kg >= 0.0),
    billed_rate              REAL NOT NULL DEFAULT 0.0 CHECK (billed_rate >= 0.0),
    billed_base_freight      REAL NOT NULL DEFAULT 0.0 CHECK (billed_base_freight >= 0.0),
    billed_toll_charges      REAL NOT NULL DEFAULT 0.0 CHECK (billed_toll_charges >= 0.0),
    billed_detention_charges REAL NOT NULL DEFAULT 0.0 CHECK (billed_detention_charges >= 0.0),
    billed_total             REAL NOT NULL DEFAULT 0.0 CHECK (billed_total >= 0.0),

    -- Audited ground-truth metrics
    gps_distance_km          REAL NOT NULL DEFAULT 0.0 CHECK (gps_distance_km >= 0.0),
    actual_weight_kg         REAL NOT NULL DEFAULT 0.0 CHECK (actual_weight_kg >= 0.0),
    audited_rate             REAL NOT NULL DEFAULT 0.0 CHECK (audited_rate >= 0.0),
    audited_base_freight     REAL NOT NULL DEFAULT 0.0 CHECK (audited_base_freight >= 0.0),
    audited_toll_charges     REAL NOT NULL DEFAULT 0.0 CHECK (audited_toll_charges >= 0.0),
    audited_detention_hours  REAL NOT NULL DEFAULT 0.0 CHECK (audited_detention_hours >= 0.0),
    audited_detention_charges REAL NOT NULL DEFAULT 0.0 CHECK (audited_detention_charges >= 0.0),
    audited_total            REAL NOT NULL DEFAULT 0.0 CHECK (audited_total >= 0.0),

    -- Variances & overbilling calculation
    distance_variance_km     REAL NOT NULL DEFAULT 0.0,
    weight_variance_kg       REAL NOT NULL DEFAULT 0.0,
    rate_variance            REAL NOT NULL DEFAULT 0.0,
    toll_variance            REAL NOT NULL DEFAULT 0.0,
    overbilling_amount       REAL NOT NULL DEFAULT 0.0,
    variance_pct             REAL NOT NULL DEFAULT 0.0,

    -- Deductions & settlement
    deduction_amount         REAL NOT NULL DEFAULT 0.0 CHECK (deduction_amount >= 0.0),
    payable_amount           REAL NOT NULL DEFAULT 0.0 CHECK (payable_amount >= 0.0),
    deduction_reason         TEXT NOT NULL DEFAULT '',
    debit_note_number        TEXT NOT NULL DEFAULT '',

    -- Audit trail & resolution
    discrepancy_flags        TEXT NOT NULL DEFAULT '',
    dispute_reason           TEXT NOT NULL DEFAULT '',
    reviewed_by              TEXT NOT NULL DEFAULT '',
    reviewed_at              DATETIME,
    approved_by              TEXT NOT NULL DEFAULT '',
    approved_at              DATETIME,
    created_at               DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at               DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    FOREIGN KEY (trip_id) REFERENCES trips(id),
    FOREIGN KEY (invoice_id) REFERENCES invoices(id),
    FOREIGN KEY (booking_id) REFERENCES bookings(id),
    FOREIGN KEY (rate_card_id) REFERENCES freight_rate_cards(id)
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
    tenant_id        TEXT NOT NULL,
    audit_id         TEXT NOT NULL,
    category         TEXT NOT NULL CHECK (category IN ('DISTANCE_INFLATION', 'WEIGHT_MISMATCH', 'RATE_OVERCHARGE', 'UNVERIFIED_TOLL', 'EXCESS_DETENTION', 'DAMAGE_PENALTY', 'OTHER')),
    description      TEXT NOT NULL,
    claimed_value    REAL NOT NULL DEFAULT 0.0,
    verified_value   REAL NOT NULL DEFAULT 0.0,
    unit_rate        REAL NOT NULL DEFAULT 0.0,
    deduction_amount REAL NOT NULL CHECK (deduction_amount >= 0.0),
    created_at       DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    FOREIGN KEY (audit_id) REFERENCES freight_audits(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_freight_deductions_audit
    ON freight_audit_deductions(tenant_id, audit_id);

-- Tenant FK Triggers
-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_freight_rate_cards_tenant_fk_insert
BEFORE INSERT ON freight_rate_cards
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for freight_rate_cards.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_freight_rate_cards_tenant_fk_update
BEFORE UPDATE OF tenant_id ON freight_rate_cards
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for freight_rate_cards.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_freight_audits_tenant_fk_insert
BEFORE INSERT ON freight_audits
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for freight_audits.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_freight_audits_tenant_fk_update
BEFORE UPDATE OF tenant_id ON freight_audits
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for freight_audits.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_freight_deductions_tenant_fk_insert
BEFORE INSERT ON freight_audit_deductions
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for freight_audit_deductions.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_freight_deductions_tenant_fk_update
BEFORE UPDATE OF tenant_id ON freight_audit_deductions
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for freight_audit_deductions.tenant_id') END;
END;
-- +goose StatementEnd

-- RBAC Seeding & Grants
INSERT OR IGNORE INTO permissions (name, description) VALUES
('freight_audit:read', 'View freight audit reports, rate cards, and billing variance calculations'),
('freight_audit:write', 'Run freight audits, approve overbilling deductions, manage rate cards, and issue debit notes');

-- Grant to Role 1 (Admin)
INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT 1, id FROM permissions WHERE name IN ('freight_audit:read', 'freight_audit:write');

-- Grant to Role 2 (Dispatcher / Ops)
INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT 2, id FROM permissions WHERE name IN ('freight_audit:read', 'freight_audit:write');

-- Grant to Role 6 (Org Admin)
INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT 6, id FROM permissions WHERE name IN ('freight_audit:read', 'freight_audit:write');

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN (SELECT id FROM permissions WHERE name IN ('freight_audit:read', 'freight_audit:write'));
DELETE FROM permissions WHERE name IN ('freight_audit:read', 'freight_audit:write');

DROP TRIGGER IF EXISTS trg_freight_deductions_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_freight_deductions_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_freight_audits_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_freight_audits_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_freight_rate_cards_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_freight_rate_cards_tenant_fk_insert;

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
