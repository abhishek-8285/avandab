-- +goose Up
-- 00185 — Driver KYC post-tenure retention (published DPDP schedule:
-- "Driver KYC & Licences — Employment Duration + 3 Years").
-- deactivated_at starts the clock when a driver becomes inactive; the
-- retention worker clears KYC fields and driver_documents 3 years later and
-- stamps kyc_purged_at for idempotency. Existing inactive drivers are
-- backfilled from updated_at (best effort; a recent update only delays purge).
ALTER TABLE drivers ADD COLUMN deactivated_at DATETIME;
ALTER TABLE drivers ADD COLUMN kyc_purged_at DATETIME;
UPDATE drivers SET deactivated_at = updated_at WHERE status = 'inactive' AND deactivated_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_drivers_deactivated_at ON drivers(deactivated_at);

-- +goose Down
DROP INDEX IF EXISTS idx_drivers_deactivated_at;
ALTER TABLE drivers DROP COLUMN kyc_purged_at;
ALTER TABLE drivers DROP COLUMN deactivated_at;
