-- +goose Up
-- 00100 Customer parity with Fleetbase Contacts + Customers (Spec gap § fleetbase.io check)
-- Adds fleetbase-style fields + ARCH promised fields missing from 00004.
-- All ADD COLUMN are nullable/with defaults to keep existing rows valid.
-- tenant_id backfill uses shared.DefaultTenant '1'.

-- Core identity / Fleetbase parity
ALTER TABLE customers ADD COLUMN customer_code TEXT;
ALTER TABLE customers ADD COLUMN title TEXT;
ALTER TABLE customers ADD COLUMN contact_person TEXT;
ALTER TABLE customers ADD COLUMN internal_id TEXT;

-- Fleetbase structured location + avatar + extensibility
ALTER TABLE customers ADD COLUMN photo_url TEXT;
ALTER TABLE customers ADD COLUMN place_uuid TEXT;
ALTER TABLE customers ADD COLUMN meta TEXT NOT NULL DEFAULT '{}';

-- Addressing
ALTER TABLE customers ADD COLUMN billing_address TEXT;

-- Classification / lifecycle
ALTER TABLE customers ADD COLUMN type TEXT NOT NULL DEFAULT 'individual' CHECK (type IN ('individual','company','customer','supplier','facilitator','contact'));
ALTER TABLE customers ADD COLUMN status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','inactive'));

-- Commercial
ALTER TABLE customers ADD COLUMN payment_terms_days INTEGER NOT NULL DEFAULT 0;
ALTER TABLE customers ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '1';

-- Backfill customer_code for existing rows (stable per-id, unique)
UPDATE customers SET customer_code = 'CUST-' || substr(replace(id,'-',''),1,8) || substr(replace(id,'-',''),9,4) WHERE customer_code IS NULL;
UPDATE customers SET internal_id = customer_code WHERE internal_id IS NULL;

-- Ensure uniqueness where not already indexed (partial to allow NULL legacy? now filled)
CREATE UNIQUE INDEX IF NOT EXISTS idx_customers_customer_code ON customers(customer_code);
CREATE INDEX IF NOT EXISTS idx_customers_internal_id ON customers(internal_id);
CREATE INDEX IF NOT EXISTS idx_customers_tenant_id ON customers(tenant_id);
CREATE INDEX IF NOT EXISTS idx_customers_type ON customers(type);
CREATE INDEX IF NOT EXISTS idx_customers_status ON customers(status);
CREATE INDEX IF NOT EXISTS idx_customers_place_uuid ON customers(place_uuid);

-- Tenant-scoped uniqueness for phone/email via partial indexes is enforced at service layer
-- (SQLite cannot easily alter with adding UNIQUE on existing column nullable, handled in app).

-- +goose Down
DROP INDEX IF EXISTS idx_customers_place_uuid;
DROP INDEX IF EXISTS idx_customers_status;
DROP INDEX IF EXISTS idx_customers_type;
DROP INDEX IF EXISTS idx_customers_tenant_id;
DROP INDEX IF EXISTS idx_customers_internal_id;
DROP INDEX IF EXISTS idx_customers_customer_code;
ALTER TABLE customers DROP COLUMN tenant_id;
ALTER TABLE customers DROP COLUMN payment_terms_days;
ALTER TABLE customers DROP COLUMN status;
ALTER TABLE customers DROP COLUMN type;
ALTER TABLE customers DROP COLUMN billing_address;
ALTER TABLE customers DROP COLUMN meta;
ALTER TABLE customers DROP COLUMN place_uuid;
ALTER TABLE customers DROP COLUMN photo_url;
ALTER TABLE customers DROP COLUMN internal_id;
ALTER TABLE customers DROP COLUMN contact_person;
ALTER TABLE customers DROP COLUMN title;
ALTER TABLE customers DROP COLUMN customer_code;
