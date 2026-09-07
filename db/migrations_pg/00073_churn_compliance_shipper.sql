-- PG port of 00073_churn_compliance_shipper.sql | status: PORTABLE | flags: none
-- +goose Up
-- 00073_churn_compliance_shipper.sql
CREATE TABLE IF NOT EXISTS customer_users (
    id TEXT PRIMARY KEY,
    customer_id TEXT NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL DEFAULT (CURRENT_TIMESTAMP),
    UNIQUE(customer_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_customer_users_user ON customer_users(user_id);
CREATE INDEX IF NOT EXISTS idx_customer_users_customer ON customer_users(customer_id);
-- Enforce dispatch blocker audit (override log)
CREATE TABLE IF NOT EXISTS dispatch_overrides (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL DEFAULT '1',
    trip_id TEXT NOT NULL,
    vehicle_id TEXT,
    driver_id TEXT,
    blocked_by TEXT NOT NULL,
    reason TEXT NOT NULL,
    overridden_by TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (CURRENT_TIMESTAMP)
);
-- shipper feedback
CREATE TABLE IF NOT EXISTS trip_feedback (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL DEFAULT '1',
    trip_id TEXT NOT NULL REFERENCES trips(id),
    customer_id TEXT NOT NULL,
    rating INTEGER NOT NULL CHECK (rating BETWEEN 1 AND 5),
    comment TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (CURRENT_TIMESTAMP)
);
-- POD hardening columns (if not exists from 00055)
-- Note: pod_consignee_phone already exists via 00032_kharcha_and_epod.sql (db/migrations/00032_kharcha_and_epod.sql:16)
-- Spec §3's ALTER for that column would fail duplicate-column; omitted for idempotency (verified: transport.db PRAGMA table_info(trips) has pod_consignee_phone).
ALTER TABLE trips ADD COLUMN pod_signature_data TEXT;
ALTER TABLE trips ADD COLUMN pod_quantity_short REAL DEFAULT 0;
ALTER TABLE trips ADD COLUMN pod_damage_qty REAL DEFAULT 0;
ALTER TABLE trips ADD COLUMN pod_refusal_reason TEXT;
-- RBAC: permissions table is (name, description) per 00001_initial.sql:12 (not resource/action). Fix spec DDL to match ground truth.
INSERT INTO permissions (name, description) VALUES
    ('customer_portal:read','Shipper view own bookings/invoices/tracking'),
    ('customer_portal:write','Shipper feedback') ON CONFLICT DO NOTHING;
-- Ensure customer role exists (spec assumes 'customer' but roles seed only admin/dispatcher/accountant/viewer/driver/org_admin)
INSERT INTO roles (name, description) VALUES ('customer','Shipper portal customer') ON CONFLICT DO NOTHING;
INSERT INTO role_permissions (role_id, permission_id)
    SELECT r.id, p.id FROM roles r, permissions p
    WHERE r.name='customer' AND p.name LIKE 'customer_portal:%' ON CONFLICT DO NOTHING;
-- +goose Down
DROP TABLE IF EXISTS trip_feedback;
DROP TABLE IF EXISTS dispatch_overrides;
DROP TABLE IF EXISTS customer_users;
ALTER TABLE trips DROP COLUMN pod_signature_data;
ALTER TABLE trips DROP COLUMN pod_quantity_short;
ALTER TABLE trips DROP COLUMN pod_damage_qty;
ALTER TABLE trips DROP COLUMN pod_refusal_reason;
-- pod_consignee_phone not dropped here: pre-existing via 00032, not owned by this migration (see Up note)
