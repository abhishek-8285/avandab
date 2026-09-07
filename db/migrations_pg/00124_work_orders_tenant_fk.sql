-- PG port of 00124_work_orders_tenant_fk.sql | status: MANUAL | flags: MANUAL-TRIGGER-GEN | reviewed: YES
-- +goose Up
-- Shared guard: one function for all tenant-FK triggers. TG_TABLE_NAME
-- reproduces the sqlite per-table error text.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION tenant_fk_guard_fn()
RETURNS trigger AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id) THEN
        RAISE EXCEPTION 'FK violation: tenants(id) missing for %.tenant_id', TG_TABLE_NAME;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
-- 00124 — work_orders tenant FK hardening (00103 convention, applied late).
-- 00123 created work_orders without the trigger-based tenant enforcement
-- that is required from 00103 onwards; this migration adds it without
-- touching 00123 (append-only rule). No new indexes: 00123 already ships
-- idx_work_orders_tenant_status and idx_work_orders_vehicle.
DROP TRIGGER IF EXISTS trg_work_orders_tenant_fk_insert ON work_orders;
CREATE TRIGGER trg_work_orders_tenant_fk_insert BEFORE INSERT ON work_orders
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_work_orders_tenant_fk_update ON work_orders;
CREATE TRIGGER trg_work_orders_tenant_fk_update BEFORE UPDATE OF TENANT_ID ON work_orders
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
-- +goose Down
-- Shared guard: one function for all tenant-FK triggers. TG_TABLE_NAME
-- reproduces the sqlite per-table error text.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION tenant_fk_guard_fn()
RETURNS trigger AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id) THEN
        RAISE EXCEPTION 'FK violation: tenants(id) missing for %.tenant_id', TG_TABLE_NAME;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
DROP TRIGGER IF EXISTS trg_work_orders_tenant_fk_update ON work_orders;
DROP TRIGGER IF EXISTS trg_work_orders_tenant_fk_insert ON work_orders;
