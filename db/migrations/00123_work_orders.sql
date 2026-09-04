-- +goose Up
-- 00123 — Work orders (job cards) for maintenance (P5 feature layer).
-- Schedules detect; records close the books; work orders track the job in
-- between: open → assigned → in_progress → done | cancelled (+ on_hold).
-- Tenant-scoped throughout (tenant_id NOT NULL, no global reads).

CREATE TABLE IF NOT EXISTS work_orders (
    id             TEXT PRIMARY KEY,
    tenant_id      TEXT NOT NULL,
    vehicle_id     TEXT NOT NULL,
    schedule_id    TEXT,
    trip_id        TEXT,
    title          TEXT NOT NULL,
    description    TEXT NOT NULL DEFAULT '',
    assignee       TEXT NOT NULL DEFAULT '',
    vendor         TEXT NOT NULL DEFAULT '',
    cost_estimate  REAL,
    cost_actual    REAL,
    status         TEXT NOT NULL DEFAULT 'open'
                   CHECK (status IN ('open','assigned','in_progress','on_hold','done','cancelled')),
    due_at         DATETIME,
    closed_at      DATETIME,
    created_at     DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at     DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (vehicle_id) REFERENCES vehicles(id),
    FOREIGN KEY (schedule_id) REFERENCES maintenance_schedules(id),
    FOREIGN KEY (trip_id) REFERENCES trips(id)
);

CREATE INDEX IF NOT EXISTS idx_work_orders_tenant_status
    ON work_orders(tenant_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_work_orders_vehicle
    ON work_orders(vehicle_id, status);

-- +goose Down
DROP INDEX IF EXISTS idx_work_orders_vehicle;
DROP INDEX IF EXISTS idx_work_orders_tenant_status;
DROP TABLE IF EXISTS work_orders;
