-- PG port of 00169_provider_mode_provenance.sql | status: PORTABLE | flags: none
-- +goose Up
-- SQL in this section is executed when the migration is applied.

ALTER TABLE eway_bills ADD COLUMN provider_mode VARCHAR(20) NOT NULL DEFAULT 'LIVE' CHECK (provider_mode IN ('LIVE', 'MOCK'));
ALTER TABLE fastag_transactions ADD COLUMN provider_mode VARCHAR(20) NOT NULL DEFAULT 'LIVE' CHECK (provider_mode IN ('LIVE', 'MOCK'));
ALTER TABLE driver_expenses ADD COLUMN provider_mode VARCHAR(20) NOT NULL DEFAULT 'LIVE' CHECK (provider_mode IN ('LIVE', 'MOCK'));

-- +goose Down
ALTER TABLE driver_expenses DROP COLUMN provider_mode;
ALTER TABLE fastag_transactions DROP COLUMN provider_mode;
ALTER TABLE eway_bills DROP COLUMN provider_mode;
