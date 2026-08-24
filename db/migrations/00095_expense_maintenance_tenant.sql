-- +goose Up
-- Spec 22 S2 fix: pnl_service (and the console money strip) filter
-- driver_expenses and maintenance_records by tenant_id, but neither table
-- had that column — fuel and maintenance costs silently aggregated to zero.
-- Adds the columns with the bootstrap default (pattern per 00016–00019)
-- so existing rows attribute to the single-tenant deployment.
ALTER TABLE driver_expenses ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '1';

ALTER TABLE maintenance_records ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '1';

CREATE INDEX idx_driver_expenses_tenant_date
  ON driver_expenses(tenant_id, category, created_at);
CREATE INDEX idx_maintenance_records_tenant_date
  ON maintenance_records(tenant_id, performed_at);

-- +goose Down
DROP INDEX IF EXISTS idx_maintenance_records_tenant_date;
DROP INDEX IF EXISTS idx_driver_expenses_tenant_date;
ALTER TABLE maintenance_records DROP COLUMN tenant_id;
ALTER TABLE driver_expenses DROP COLUMN tenant_id;
