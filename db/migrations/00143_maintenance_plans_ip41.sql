-- +goose Up
-- 00143 — Fleet maintenance IP41 SOP parity (pp.10-15)
-- Maintenance plans (Single Cycle Plans IP41) with projection from annual_estimate (00126)
-- and job card (work_orders) generation (00123).

CREATE TABLE IF NOT EXISTS maintenance_plans (
    id                    TEXT PRIMARY KEY,
    tenant_id             TEXT NOT NULL,
    plan_number           TEXT NOT NULL,
    vehicle_id            TEXT NOT NULL REFERENCES vehicles(id),
    measuring_point_id    TEXT REFERENCES vehicle_measuring_points(id),
    service_type          TEXT NOT NULL,
    description           TEXT NOT NULL DEFAULT '',
    cycle_interval_km     REAL,
    cycle_interval_days   INTEGER,
    call_horizon_percent  REAL NOT NULL DEFAULT 100.0,
    last_scheduled_km     REAL,
    last_scheduled_date   DATETIME,
    next_due_km           REAL,
    next_due_date         DATETIME,
    status                TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')),
    created_at            DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at            DATETIME NOT NULL DEFAULT (datetime('now')),
    UNIQUE(tenant_id, plan_number)
);

CREATE INDEX IF NOT EXISTS idx_maint_plans_tenant_vehicle ON maintenance_plans(tenant_id, vehicle_id);
CREATE INDEX IF NOT EXISTS idx_maint_plans_tenant_status  ON maintenance_plans(tenant_id, status);

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_maint_plans_tenant_fk_insert
BEFORE INSERT ON maintenance_plans
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for maintenance_plans.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_maint_plans_tenant_fk_update
BEFORE UPDATE OF tenant_id ON maintenance_plans
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for maintenance_plans.tenant_id') END;
END;
-- +goose StatementEnd

ALTER TABLE work_orders ADD COLUMN plan_id TEXT REFERENCES maintenance_plans(id);
ALTER TABLE work_orders ADD COLUMN due_km REAL;

-- +goose Down
DROP TRIGGER IF EXISTS trg_maint_plans_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_maint_plans_tenant_fk_insert;
DROP INDEX IF EXISTS idx_maint_plans_tenant_status;
DROP INDEX IF EXISTS idx_maint_plans_tenant_vehicle;
DROP TABLE IF EXISTS maintenance_plans;
ALTER TABLE work_orders DROP COLUMN due_km;
ALTER TABLE work_orders DROP COLUMN plan_id;
