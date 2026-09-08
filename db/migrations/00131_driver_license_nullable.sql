-- +goose Up
-- +goose NO TRANSACTION
-- 00131 — drivers.license_number / license_expiry go nullable (unknown license
-- is NULL, never a 'DL-PENDING' + invented +5y placeholder). Registration flows
-- (DriverAppService.RegisterDriver, self-register driver link) insert NULLs;
-- SubmitLicense syncs the row with the submitted number/expiry, so the shadow
-- copy turns real exactly when the driver hands over a license.
-- SQLite cannot ALTER nullability: rebuild pattern per 00081/00126. Column
-- ORDER matches the live DDL of 00002+00018+00021+00046 (incl. score/tier from
-- 00043 Up) for the explicit INSERT...SELECT. Backfill converts every legacy
-- 'DL-PENDING' row to NULL/NULL — those expiries were fabricated at
-- registration time and must not survive.
--
-- NO TRANSACTION + PRAGMA foreign_keys=OFF is REQUIRED here (not optional):
-- drivers is a referenced parent and app-wide FK enforcement is ON, so DROP
-- TABLE drivers inside a tx fails with SQLITE_CONSTRAINT_FOREIGNKEY.

PRAGMA foreign_keys=OFF;

CREATE TABLE drivers_rebuild_00131 (
    id                  TEXT PRIMARY KEY,
    driver_id           TEXT NOT NULL UNIQUE,
    first_name          TEXT NOT NULL,
    last_name           TEXT NOT NULL,
    phone               TEXT NOT NULL,
    email               TEXT,
    address             TEXT,
    license_number      TEXT,
    license_expiry      DATE,
    experience_years    INTEGER NOT NULL DEFAULT 0,
    status              TEXT NOT NULL DEFAULT 'available' CHECK (status IN ('available', 'on_trip', 'leave', 'inactive')),
    emergency_contact_name TEXT,
    emergency_contact_phone TEXT,
    notes               TEXT,
    created_at          DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at          DATETIME NOT NULL DEFAULT (datetime('now'))
, tenant_id TEXT DEFAULT '1' NOT NULL, version INTEGER NOT NULL DEFAULT 1, blocked INTEGER NOT NULL DEFAULT 0, blocked_reason TEXT, aadhaar TEXT, pan TEXT, bank_details TEXT, score REAL, tier TEXT);

INSERT INTO drivers_rebuild_00131 (id, driver_id, first_name, last_name, phone, email, address, license_number, license_expiry, experience_years, status, emergency_contact_name, emergency_contact_phone, notes, created_at, updated_at, tenant_id, version, blocked, blocked_reason, aadhaar, pan, bank_details, score, tier)
SELECT id, driver_id, first_name, last_name, phone, email, address, license_number, license_expiry, experience_years, status, emergency_contact_name, emergency_contact_phone, notes, created_at, updated_at, tenant_id, version, blocked, blocked_reason, aadhaar, pan, bank_details, score, tier FROM drivers;
DROP TABLE drivers;
ALTER TABLE drivers_rebuild_00131 RENAME TO drivers;

CREATE INDEX IF NOT EXISTS idx_drivers_tenant ON drivers(tenant_id);

-- +goose StatementBegin
CREATE TRIGGER trg_drivers_tenant_fk_insert
BEFORE INSERT ON drivers
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for drivers.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_drivers_tenant_fk_update
BEFORE UPDATE OF tenant_id ON drivers
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for drivers.tenant_id') END;
END;
-- +goose StatementEnd

-- Fabricated placeholders become honest NULLs (number and expiry alike: the
-- expiry on a DL-PENDING row was always invented at registration time).
UPDATE drivers SET license_number = NULL, license_expiry = NULL WHERE license_number = 'DL-PENDING';

-- +goose Down
-- +goose NO TRANSACTION
-- Restore NOT NULL. Rows left without a license regain the legacy
-- 'DL-PENDING' sentinel so the copy back cannot violate the constraint.
-- Same NO TRANSACTION rationale as Up: drivers is a referenced parent.
PRAGMA foreign_keys=OFF;
UPDATE drivers SET license_number = 'DL-PENDING', license_expiry = date('now','+5 years') WHERE license_number IS NULL OR license_expiry IS NULL;

ALTER TABLE drivers RENAME TO drivers_down_00131;
CREATE TABLE drivers (
    id                  TEXT PRIMARY KEY,
    driver_id           TEXT NOT NULL UNIQUE,
    first_name          TEXT NOT NULL,
    last_name           TEXT NOT NULL,
    phone               TEXT NOT NULL,
    email               TEXT,
    address             TEXT,
    license_number      TEXT NOT NULL,
    license_expiry      DATE NOT NULL,
    experience_years    INTEGER NOT NULL DEFAULT 0,
    status              TEXT NOT NULL DEFAULT 'available' CHECK (status IN ('available', 'on_trip', 'leave', 'inactive')),
    emergency_contact_name TEXT,
    emergency_contact_phone TEXT,
    notes               TEXT,
    created_at          DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at          DATETIME NOT NULL DEFAULT (datetime('now'))
, tenant_id TEXT DEFAULT '1' NOT NULL, version INTEGER NOT NULL DEFAULT 1, blocked INTEGER NOT NULL DEFAULT 0, blocked_reason TEXT, aadhaar TEXT, pan TEXT, bank_details TEXT, score REAL, tier TEXT);

INSERT INTO drivers (id, driver_id, first_name, last_name, phone, email, address, license_number, license_expiry, experience_years, status, emergency_contact_name, emergency_contact_phone, notes, created_at, updated_at, tenant_id, version, blocked, blocked_reason, aadhaar, pan, bank_details, score, tier)
SELECT id, driver_id, first_name, last_name, phone, email, address, license_number, license_expiry, experience_years, status, emergency_contact_name, emergency_contact_phone, notes, created_at, updated_at, tenant_id, version, blocked, blocked_reason, aadhaar, pan, bank_details, score, tier FROM drivers_down_00131;
DROP TABLE drivers_down_00131;

CREATE INDEX IF NOT EXISTS idx_drivers_tenant ON drivers(tenant_id);

-- +goose StatementBegin
CREATE TRIGGER trg_drivers_tenant_fk_insert
BEFORE INSERT ON drivers
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for drivers.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_drivers_tenant_fk_update
BEFORE UPDATE OF tenant_id ON drivers
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for drivers.tenant_id') END;
END;
-- +goose StatementEnd
