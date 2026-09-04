-- +goose Up
-- 00125 — per-tenant company profiles (multi-tenant settings isolation).
-- company_settings stays the PLATFORM-global default singleton (created ONCE
-- at 00001/00042, append-only rule): this migration ADDS a new table, never
-- touches the old one. Reads resolve tenant row first, global id=1 fallback.
-- Tenants created after this migration start with NO row (blank profile forces
-- /company/onboard); pre-existing tenants are backfilled from the singleton so
-- no org loses its visible data on upgrade.

CREATE TABLE IF NOT EXISTS tenant_company_profiles (
    tenant_id      TEXT PRIMARY KEY,
    company_name   TEXT NOT NULL DEFAULT '',
    logo_path      TEXT,
    currency       TEXT NOT NULL DEFAULT 'INR',
    timezone       TEXT NOT NULL DEFAULT 'Asia/Kolkata',
    gst_enabled    BOOLEAN NOT NULL DEFAULT 0,
    gst_rate       REAL NOT NULL DEFAULT 0.0,
    booking_prefix TEXT NOT NULL DEFAULT 'BK',
    trip_prefix    TEXT NOT NULL DEFAULT 'TR',
    invoice_prefix TEXT NOT NULL DEFAULT 'INV',
    financial_year TEXT,
    address        TEXT,
    phone          TEXT,
    email          TEXT,
    gst_number     TEXT,
    pan_number     TEXT,
    state_code     TEXT NOT NULL DEFAULT '27',
    created_at     DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at     DATETIME NOT NULL DEFAULT (datetime('now'))
);

-- No separate index: tenant_id is the PRIMARY KEY (auto-indexed).
-- FK triggers follow the 00103/00104 fail-closed convention: WHEN IS NOT
-- NULL only, so even '' trips RAISE(ABORT) instead of bypassing the check.

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_tenant_company_profiles_fk_insert
BEFORE INSERT ON tenant_company_profiles
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for tenant_company_profiles.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_tenant_company_profiles_fk_update
BEFORE UPDATE OF tenant_id ON tenant_company_profiles
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for tenant_company_profiles.tenant_id') END;
END;
-- +goose StatementEnd

-- Backfill: every existing NON-BOOTSTRAP tenant inherits the current
-- singleton values so the upgrade changes nothing visible; each org diverges
-- independently afterwards. Tenant '1' (DefaultTenant) is deliberately
-- excluded: it stays global-backed so single-tenant deployments and all
-- existing tests that write company_settings id=1 keep working unchanged.
INSERT OR IGNORE INTO tenant_company_profiles (
    tenant_id, company_name, logo_path, currency, timezone,
    gst_enabled, gst_rate, booking_prefix, trip_prefix, invoice_prefix,
    financial_year, address, phone, email, gst_number, pan_number, state_code
)
SELECT
    t.id, cs.company_name, cs.logo_path, cs.currency, cs.timezone,
    cs.gst_enabled, cs.gst_rate, cs.booking_prefix, cs.trip_prefix, cs.invoice_prefix,
    cs.financial_year, cs.address, cs.phone, cs.email, cs.gst_number, cs.pan_number, cs.state_code
FROM tenants t, company_settings cs
WHERE cs.id = 1 AND t.id != '1';

-- +goose Down
DROP TRIGGER IF EXISTS trg_tenant_company_profiles_fk_update;
DROP TRIGGER IF EXISTS trg_tenant_company_profiles_fk_insert;
DROP TABLE IF EXISTS tenant_company_profiles;
