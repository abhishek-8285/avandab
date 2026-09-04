-- +goose Up
-- 00124 — work_orders tenant FK hardening (00103 convention, applied late).
-- 00123 created work_orders without the trigger-based tenant enforcement
-- that is required from 00103 onwards; this migration adds it without
-- touching 00123 (append-only rule). No new indexes: 00123 already ships
-- idx_work_orders_tenant_status and idx_work_orders_vehicle.

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_work_orders_tenant_fk_insert
BEFORE INSERT ON work_orders
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for work_orders.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_work_orders_tenant_fk_update
BEFORE UPDATE OF tenant_id ON work_orders
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for work_orders.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS trg_work_orders_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_work_orders_tenant_fk_insert;
