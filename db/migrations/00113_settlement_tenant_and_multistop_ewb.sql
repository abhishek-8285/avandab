-- +goose Up
-- Priority 5B: Multi-tenant driver settlements & multi-stop EWB stage tracking
ALTER TABLE driver_settlements ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '1';
ALTER TABLE driver_settlements ADD COLUMN commission_rate REAL NOT NULL DEFAULT 0.0;
ALTER TABLE driver_settlements ADD COLUMN toll_adjustment REAL NOT NULL DEFAULT 0.0;
ALTER TABLE driver_settlements ADD COLUMN advance_deductions REAL NOT NULL DEFAULT 0.0;
CREATE INDEX IF NOT EXISTS idx_driver_settlements_tenant_trip ON driver_settlements(tenant_id, trip_id);

-- +goose Down
DROP INDEX IF EXISTS idx_driver_settlements_tenant_trip;
ALTER TABLE driver_settlements DROP COLUMN advance_deductions;
ALTER TABLE driver_settlements DROP COLUMN toll_adjustment;
ALTER TABLE driver_settlements DROP COLUMN commission_rate;
ALTER TABLE driver_settlements DROP COLUMN tenant_id;
