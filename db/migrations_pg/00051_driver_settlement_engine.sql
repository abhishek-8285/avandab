-- PG port of 00051_driver_settlement_engine.sql | status: PORTABLE | flags: none
-- +goose Up
-- Feature: make driver settlements real + TDS + settlement lines.
-- Add TDS + commission + rate-model columns to the existing driver_settlements table (00030).
ALTER TABLE driver_settlements ADD COLUMN commission_amount REAL NOT NULL DEFAULT 0.0;
ALTER TABLE driver_settlements ADD COLUMN tds_rate         REAL NOT NULL DEFAULT 0.0;
ALTER TABLE driver_settlements ADD COLUMN tds_amount       REAL NOT NULL DEFAULT 0.0;
ALTER TABLE driver_settlements ADD COLUMN rate_model       TEXT NOT NULL DEFAULT 'fixed';
ALTER TABLE driver_settlements ADD COLUMN rate_basis_json  TEXT;
ALTER TABLE driver_settlements ADD COLUMN confirmed_at     TIMESTAMPTZ;
ALTER TABLE driver_settlements ADD COLUMN disputed_at      TIMESTAMPTZ;
ALTER TABLE driver_settlements ADD COLUMN dispute_reason   TEXT;
-- Per-line breakdown of a settlement (audit + driver statement).
CREATE TABLE IF NOT EXISTS settlement_lines (
    id             TEXT PRIMARY KEY,
    settlement_id  TEXT NOT NULL,
    trip_id        TEXT NOT NULL,
    line_type      TEXT NOT NULL CHECK (line_type IN ('gross_fare','commission','advances','deduction','tds','adjustment')),
    label          TEXT NOT NULL,
    amount         REAL NOT NULL,
    ref_id         TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (settlement_id) REFERENCES driver_settlements(id) ON DELETE CASCADE,
    FOREIGN KEY (trip_id) REFERENCES trips(id)
);
CREATE INDEX IF NOT EXISTS idx_settlement_lines_settlement ON settlement_lines(settlement_id);
CREATE INDEX IF NOT EXISTS idx_settlement_lines_trip ON settlement_lines(trip_id);
-- Rate model + TDS config (company_config owned by 00042 — seeds only).
INSERT INTO company_config (tenant_id, key, value) VALUES
('1', 'settlement_rate_model', 'per_km'),
('1', 'settlement_rate_per_km', '11.90'),
('1', 'settlement_fixed_fare', '5000.00'),
('1', 'settlement_commission_pct', '5.00'),
('1', 'tds_section', '194C'),
('1', 'tds_rate_with_pan', '1.00'),
('1', 'tds_rate_without_pan', '2.00') ON CONFLICT DO NOTHING;
-- RBAC seeds
INSERT INTO permissions (name, description) VALUES
('settlements:read', 'View driver settlements'),
('settlements:write', 'Generate settlements'),
('settlements:approve', 'Mark paid / approve') ON CONFLICT DO NOTHING;
INSERT INTO role_permissions (role_id, permission_id)
SELECT 1, id FROM permissions WHERE name LIKE 'settlements:%' ON CONFLICT DO NOTHING;
INSERT INTO role_permissions (role_id, permission_id)
SELECT 2, id FROM permissions WHERE name IN ('settlements:read','settlements:write') ON CONFLICT DO NOTHING;
-- +goose Down
DROP TABLE IF EXISTS settlement_lines;
ALTER TABLE driver_settlements DROP COLUMN commission_amount;
ALTER TABLE driver_settlements DROP COLUMN tds_rate;
ALTER TABLE driver_settlements DROP COLUMN tds_amount;
ALTER TABLE driver_settlements DROP COLUMN rate_model;
ALTER TABLE driver_settlements DROP COLUMN rate_basis_json;
ALTER TABLE driver_settlements DROP COLUMN confirmed_at;
ALTER TABLE driver_settlements DROP COLUMN disputed_at;
ALTER TABLE driver_settlements DROP COLUMN dispute_reason;
DELETE FROM company_config WHERE key IN
('settlement_rate_model','settlement_rate_per_km','settlement_fixed_fare',
 'settlement_commission_pct','tds_section','tds_rate_with_pan','tds_rate_without_pan');
DELETE FROM permissions WHERE name LIKE 'settlements:%';
