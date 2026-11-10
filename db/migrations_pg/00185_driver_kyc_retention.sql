-- +goose Up
-- PG port of 00185_driver_kyc_retention.sql
-- Driver KYC post-tenure retention (see sqlite chain).
ALTER TABLE drivers ADD COLUMN deactivated_at TIMESTAMP;
ALTER TABLE drivers ADD COLUMN kyc_purged_at TIMESTAMP;
UPDATE drivers SET deactivated_at = updated_at WHERE status = 'inactive' AND deactivated_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_drivers_deactivated_at ON drivers(deactivated_at);

-- +goose Down
DROP INDEX IF EXISTS idx_drivers_deactivated_at;
ALTER TABLE drivers DROP COLUMN kyc_purged_at;
ALTER TABLE drivers DROP COLUMN deactivated_at;
