-- PG port of 00143_maintenance_plans_ip41.sql | status: PORTABLE | flags: none
-- +goose Up
CREATE TABLE IF NOT EXISTS maintenance_plans (
    id                    TEXT PRIMARY KEY,
    tenant_id             TEXT NOT NULL,
    plan_number           TEXT NOT NULL,
    vehicle_id            TEXT NOT NULL REFERENCES vehicles(id),
    measuring_point_id    TEXT REFERENCES vehicle_measuring_points(id),
    service_type          TEXT NOT NULL,
    description           TEXT NOT NULL DEFAULT '',
    cycle_interval_km     DOUBLE PRECISION,
    cycle_interval_days   INTEGER,
    call_horizon_percent  DOUBLE PRECISION NOT NULL DEFAULT 100.0,
    last_scheduled_km     DOUBLE PRECISION,
    last_scheduled_date   TIMESTAMPTZ,
    next_due_km           DOUBLE PRECISION,
    next_due_date         TIMESTAMPTZ,
    status                TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')),
    created_at            TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(tenant_id, plan_number)
);

CREATE INDEX IF NOT EXISTS idx_maint_plans_tenant_vehicle ON maintenance_plans(tenant_id, vehicle_id);
CREATE INDEX IF NOT EXISTS idx_maint_plans_tenant_status  ON maintenance_plans(tenant_id, status);

DROP TRIGGER IF EXISTS trg_maint_plans_tenant_fk_insert ON maintenance_plans;
CREATE TRIGGER trg_maint_plans_tenant_fk_insert BEFORE INSERT ON maintenance_plans
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();

DROP TRIGGER IF EXISTS trg_maint_plans_tenant_fk_update ON maintenance_plans;
CREATE TRIGGER trg_maint_plans_tenant_fk_update BEFORE UPDATE OF tenant_id ON maintenance_plans
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();

ALTER TABLE work_orders ADD COLUMN IF NOT EXISTS plan_id TEXT REFERENCES maintenance_plans(id);
ALTER TABLE work_orders ADD COLUMN IF NOT EXISTS due_km DOUBLE PRECISION;

-- +goose Down
ALTER TABLE work_orders DROP COLUMN IF EXISTS due_km;
ALTER TABLE work_orders DROP COLUMN IF EXISTS plan_id;
DROP TRIGGER IF EXISTS trg_maint_plans_tenant_fk_update ON maintenance_plans;
DROP TRIGGER IF EXISTS trg_maint_plans_tenant_fk_insert ON maintenance_plans;
DROP INDEX IF EXISTS idx_maint_plans_tenant_status;
DROP INDEX IF EXISTS idx_maint_plans_tenant_vehicle;
DROP TABLE IF EXISTS maintenance_plans CASCADE;
