-- +goose Up
-- 00169: provider_mode provenance for eway_bills, fastag_transactions, driver_expenses (FleetX Parity P0.2).
-- Distinguishes real business writes from mock-provider generation so mock/stub data
-- cannot contaminate live analytics, accounting, or audits.

ALTER TABLE eway_bills ADD COLUMN provider_mode TEXT NOT NULL DEFAULT 'LIVE' CHECK (provider_mode IN ('LIVE', 'MOCK'));
ALTER TABLE fastag_transactions ADD COLUMN provider_mode TEXT NOT NULL DEFAULT 'LIVE' CHECK (provider_mode IN ('LIVE', 'MOCK'));
ALTER TABLE driver_expenses ADD COLUMN provider_mode TEXT NOT NULL DEFAULT 'LIVE' CHECK (provider_mode IN ('LIVE', 'MOCK'));

-- +goose Down
ALTER TABLE driver_expenses DROP COLUMN provider_mode;
ALTER TABLE fastag_transactions DROP COLUMN provider_mode;
ALTER TABLE eway_bills DROP COLUMN provider_mode;
