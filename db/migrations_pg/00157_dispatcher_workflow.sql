-- +goose Up
-- 00157: Dispatcher workflow — Planner Run, Planned Routes, Planned Stops, Dispatch Exceptions
-- PG port of 00157_dispatcher_workflow.sql
-- Spec: docs/design/dispatcher-route-planner/03-cto-architecture.md §2
-- Note: Postgres uses timestamptz; SQLite uses datetime('now')

CREATE TABLE IF NOT EXISTS planner_runs (
    id           TEXT PRIMARY KEY,
    tenant_id    TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    status       TEXT NOT NULL DEFAULT 'draft'
                  CHECK (status IN ('draft','planned','assigned','dispatched','done','cancelled')),
    job_id       TEXT,
    source       TEXT NOT NULL DEFAULT 'orders',
    kpi_json     TEXT,
    created_by   TEXT REFERENCES users(id),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    committed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_planner_runs_tenant    ON planner_runs(tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_planner_runs_status     ON planner_runs(tenant_id, status);
CREATE INDEX IF NOT EXISTS idx_planner_runs_job_id     ON planner_runs(job_id) WHERE job_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS planned_routes (
    id                    TEXT PRIMARY KEY,
    tenant_id             TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    run_id                TEXT NOT NULL REFERENCES planner_runs(id) ON DELETE CASCADE,
    seq                  INTEGER NOT NULL,
    vehicle_id            TEXT,
    driver_id             TEXT,
    suggested_vehicle_id  TEXT,
    suggested_driver_id   TEXT,
    total_km              REAL,
    total_min             REAL,
    toll_cost_est         REAL,
    status                TEXT NOT NULL DEFAULT 'planned'
                  CHECK (status IN ('planned','assigned','dispatched','in_progress','completed','cancelled')),
    created_at            TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_planned_routes_run     ON planned_routes(tenant_id, run_id);
CREATE INDEX IF NOT EXISTS idx_planned_routes_vehicle ON planned_routes(tenant_id, vehicle_id) WHERE vehicle_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_planned_routes_driver  ON planned_routes(tenant_id, driver_id)  WHERE driver_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS planned_stops (
    id                    TEXT PRIMARY KEY,
    tenant_id             TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    route_id              TEXT NOT NULL REFERENCES planned_routes(id) ON DELETE CASCADE,
    seq                  INTEGER NOT NULL,
    booking_id            TEXT,
    source_type           TEXT NOT NULL
                  CHECK (source_type IN ('pickup','dropoff','waypoint','backhaul')),
    address               TEXT    NOT NULL,
    lat                   REAL    NOT NULL,
    lng                   REAL    NOT NULL,
    time_window_start     TIMESTAMPTZ,
    time_window_end       TIMESTAMPTZ,
    demand                REAL,
    skills                TEXT,
    status                TEXT NOT NULL DEFAULT 'pending'
                  CHECK (status IN (
                      'pending','offered','accepted','en_route','on_site','done',
                      'missed','out_of_order','unassigned'
                  )),
    planned_eta            TIMESTAMPTZ,
    actual_eta             TIMESTAMPTZ,
    planned_duration_min   REAL,
    actual_duration_min    REAL,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_planned_stops_route    ON planned_stops(tenant_id, route_id);
CREATE INDEX IF NOT EXISTS idx_planned_stops_booking  ON planned_stops(tenant_id, booking_id) WHERE booking_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_planned_stops_status   ON planned_stops(status);

CREATE TABLE IF NOT EXISTS dispatch_exceptions (
    id           TEXT PRIMARY KEY,
    tenant_id    TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    stop_id      TEXT REFERENCES planned_stops(id),
    trip_id      TEXT,
    vehicle_id   TEXT,
    driver_id    TEXT,
    kind         TEXT NOT NULL
                  CHECK (kind IN ('late','deviation','dwell','breakdown','no_show','missed','out_of_order')),
    status       TEXT NOT NULL DEFAULT 'open'
                  CHECK (status IN ('open','acting','resolved','dismissed')),
    detail_json  TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    resolved_at  TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_dispatch_exc_tenant      ON dispatch_exceptions(tenant_id, status);
CREATE INDEX IF NOT EXISTS idx_dispatch_exc_stop        ON dispatch_exceptions(tenant_id, stop_id)  WHERE stop_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_dispatch_exc_vehicle     ON dispatch_exceptions(tenant_id, vehicle_id) WHERE vehicle_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_dispatch_exc_kind_status ON dispatch_exceptions(kind, status);

-- Tenant-scope FK enforcement via shared guard (00103).
DROP TRIGGER IF EXISTS trg_planner_runs_tenant_fk_insert ON planner_runs;
CREATE TRIGGER trg_planner_runs_tenant_fk_insert BEFORE INSERT ON planner_runs
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_planner_runs_tenant_fk_update ON planner_runs;
CREATE TRIGGER trg_planner_runs_tenant_fk_update BEFORE UPDATE OF tenant_id ON planner_runs
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_planned_routes_tenant_fk_insert ON planned_routes;
CREATE TRIGGER trg_planned_routes_tenant_fk_insert BEFORE INSERT ON planned_routes
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_planned_routes_tenant_fk_update ON planned_routes;
CREATE TRIGGER trg_planned_routes_tenant_fk_update BEFORE UPDATE OF tenant_id ON planned_routes
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_planned_stops_tenant_fk_insert ON planned_stops;
CREATE TRIGGER trg_planned_stops_tenant_fk_insert BEFORE INSERT ON planned_stops
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_planned_stops_tenant_fk_update ON planned_stops;
CREATE TRIGGER trg_planned_stops_tenant_fk_update BEFORE UPDATE OF tenant_id ON planned_stops
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_dispatch_exceptions_tenant_fk_insert ON dispatch_exceptions;
CREATE TRIGGER trg_dispatch_exceptions_tenant_fk_insert BEFORE INSERT ON dispatch_exceptions
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();
DROP TRIGGER IF EXISTS trg_dispatch_exceptions_tenant_fk_update ON dispatch_exceptions;
CREATE TRIGGER trg_dispatch_exceptions_tenant_fk_update BEFORE UPDATE OF tenant_id ON dispatch_exceptions
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();

-- +goose Down
DROP TRIGGER IF EXISTS trg_dispatch_exceptions_tenant_fk_update ON dispatch_exceptions;
DROP TRIGGER IF EXISTS trg_dispatch_exceptions_tenant_fk_insert ON dispatch_exceptions;
DROP TRIGGER IF EXISTS trg_planned_stops_tenant_fk_update ON planned_stops;
DROP TRIGGER IF EXISTS trg_planned_stops_tenant_fk_insert ON planned_stops;
DROP TRIGGER IF EXISTS trg_planned_routes_tenant_fk_update ON planned_routes;
DROP TRIGGER IF EXISTS trg_planned_routes_tenant_fk_insert ON planned_routes;
DROP TRIGGER IF EXISTS trg_planner_runs_tenant_fk_update ON planner_runs;
DROP TRIGGER IF EXISTS trg_planner_runs_tenant_fk_insert ON planner_runs;
DROP TABLE IF EXISTS dispatch_exceptions CASCADE;
DROP TABLE IF EXISTS planned_stops CASCADE;
DROP TABLE IF EXISTS planned_routes CASCADE;
DROP TABLE IF EXISTS planner_runs CASCADE;