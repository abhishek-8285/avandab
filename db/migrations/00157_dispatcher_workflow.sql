-- +goose Up
-- 00157: Dispatcher workflow — Planner Run, Planned Routes, Planned Stops, Dispatch Exceptions
-- Spec: docs/design/dispatcher-route-planner/03-cto-architecture.md §2
-- Composition: planner_runs (one planning session) → planned_routes (per-vehicle run)
--   → planned_stops (ordered stops with booking linkage) + dispatch_exceptions (actions)

-- ── planner_runs ─────────────────────────────────────────────────────────────
-- A dispatcher planning session: select orders → solver → edit → assign → dispatch.
CREATE TABLE IF NOT EXISTS planner_runs (
    id           TEXT PRIMARY KEY,
    tenant_id    TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'draft'
                  CHECK (status IN ('draft','planned','assigned','dispatched','done','cancelled')),
    job_id       TEXT,                            -- FK to route_optimization_jobs.id (solve artifact)
    source       TEXT NOT NULL DEFAULT 'orders', -- 'orders' | 'adhoc'
    kpi_json     TEXT,                            -- last what-if KPI: {total_km, total_min, total_cost, unassigned_count}
    created_by   TEXT REFERENCES users(id),
    created_at   DATETIME NOT NULL DEFAULT (datetime('now')),
    committed_at DATETIME
);

CREATE INDEX IF NOT EXISTS idx_planner_runs_tenant    ON planner_runs(tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_planner_runs_status     ON planner_runs(tenant_id, status);
CREATE INDEX IF NOT EXISTS idx_planner_runs_job_id     ON planner_runs(job_id) WHERE job_id IS NOT NULL;

-- ── planned_routes ───────────────────────────────────────────────────────────
-- One vehicle's ordered run inside a planner run.
CREATE TABLE IF NOT EXISTS planned_routes (
    id                  TEXT PRIMARY KEY,
    tenant_id           TEXT NOT NULL,
    run_id              TEXT NOT NULL REFERENCES planner_runs(id) ON DELETE CASCADE,

    seq                INTEGER NOT NULL,        -- display order within run

    -- Assignment: suggested (by ranker) vs confirmed (by dispatcher)
    vehicle_id         TEXT,
    driver_id          TEXT,
    suggested_vehicle_id TEXT,
    suggested_driver_id TEXT,

    total_km            REAL,
    total_min           REAL,
    toll_cost_est       REAL,                    -- P2 FASTag toll estimate (post-processing)
    status              TEXT NOT NULL DEFAULT 'planned'
                  CHECK (status IN ('planned','assigned','dispatched','in_progress','completed','cancelled')),

    created_at          DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_planned_routes_run     ON planned_routes(tenant_id, run_id);
CREATE INDEX IF NOT EXISTS idx_planned_routes_vehicle ON planned_routes(tenant_id, vehicle_id) WHERE vehicle_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_planned_routes_driver  ON planned_routes(tenant_id, driver_id)  WHERE driver_id IS NOT NULL;

-- ── planned_stops ────────────────────────────────────────────────────────────
-- One stop per booking / trip leg, ordered within a planned route.
CREATE TABLE IF NOT EXISTS planned_stops (
    id                TEXT PRIMARY KEY,
    tenant_id         TEXT NOT NULL,
    route_id          TEXT NOT NULL REFERENCES planned_routes(id) ON DELETE CASCADE,

    seq              INTEGER NOT NULL,          -- order within route

    booking_id       TEXT,                       -- link to bookings.id
    source_type      TEXT NOT NULL
                  CHECK (source_type IN ('pickup','dropoff','waypoint','backhaul')),

    address          TEXT    NOT NULL,
    lat              REAL    NOT NULL,
    lng              REAL    NOT NULL,

    time_window_start DATETIME,
    time_window_end   DATETIME,

    demand           REAL,                       -- capacity units
    skills           TEXT,                       -- JSON array, comma-separated

    -- Status mirrors UX doc 02 §4: pending → offered → accepted → en_route → on_site → done
    -- Plan vs actual filled from telemetry / trip state machine
    status           TEXT NOT NULL DEFAULT 'pending'
                  CHECK (status IN (
                      'pending','offered','accepted','en_route','on_site','done',
                      'missed','out_of_order','unassigned'
                  )),

    planned_eta      DATETIME,
    actual_eta       DATETIME,
    planned_duration_min REAL,
    actual_duration_min REAL,

    created_at       DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_planned_stops_route     ON planned_stops(tenant_id, route_id);
CREATE INDEX IF NOT EXISTS idx_planned_stops_booking   ON planned_stops(tenant_id, booking_id) WHERE booking_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_planned_stops_status    ON planned_stops(status);

-- ── dispatch_exceptions ──────────────────────────────────────────────────────
-- Dispatcher-actionable exceptions surfaced from telemetry/ETA/geofence signals.
CREATE TABLE IF NOT EXISTS dispatch_exceptions (
    id          TEXT PRIMARY KEY,
    tenant_id   TEXT NOT NULL,
    stop_id     TEXT REFERENCES planned_stops(id),
    trip_id     TEXT,
    vehicle_id  TEXT,
    driver_id   TEXT,

    kind        TEXT NOT NULL
                  CHECK (kind IN (
                      'late','deviation','dwell','breakdown',
                      'no_show','missed','out_of_order'
                  )),
    status      TEXT NOT NULL DEFAULT 'open'
                  CHECK (status IN ('open','acting','resolved','dismissed')),

    detail_json TEXT,                            -- {suggested_action, candidates, impact_delta}
    created_at  DATETIME NOT NULL DEFAULT (datetime('now')),
    resolved_at DATETIME
);

CREATE INDEX IF NOT EXISTS idx_dispatch_exc_tenant      ON dispatch_exceptions(tenant_id, status);
CREATE INDEX IF NOT EXISTS idx_dispatch_exc_stop        ON dispatch_exceptions(tenant_id, stop_id)  WHERE stop_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_dispatch_exc_vehicle     ON dispatch_exceptions(tenant_id, vehicle_id) WHERE vehicle_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_dispatch_exc_kind_status ON dispatch_exceptions(kind, status);

-- ── Tenant-scope FK enforcement (rule: trigger-based since 00103; sqlite) ─────
-- planner_runs
-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_planner_runs_tenant_fk_insert
BEFORE INSERT ON planner_runs
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for planner_runs.tenant_id') END;
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_planner_runs_tenant_fk_update
BEFORE UPDATE OF tenant_id ON planner_runs
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for planner_runs.tenant_id') END;
END;
-- +goose StatementEnd
-- planned_routes
-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_planned_routes_tenant_fk_insert
BEFORE INSERT ON planned_routes
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for planned_routes.tenant_id') END;
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_planned_routes_tenant_fk_update
BEFORE UPDATE OF tenant_id ON planned_routes
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for planned_routes.tenant_id') END;
END;
-- +goose StatementEnd
-- planned_stops
-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_planned_stops_tenant_fk_insert
BEFORE INSERT ON planned_stops
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for planned_stops.tenant_id') END;
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_planned_stops_tenant_fk_update
BEFORE UPDATE OF tenant_id ON planned_stops
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for planned_stops.tenant_id') END;
END;
-- +goose StatementEnd
-- dispatch_exceptions
-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_dispatch_exceptions_tenant_fk_insert
BEFORE INSERT ON dispatch_exceptions
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for dispatch_exceptions.tenant_id') END;
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_dispatch_exceptions_tenant_fk_update
BEFORE UPDATE OF tenant_id ON dispatch_exceptions
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for dispatch_exceptions.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS trg_dispatch_exceptions_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_dispatch_exceptions_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_planned_stops_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_planned_stops_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_planned_routes_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_planned_routes_tenant_fk_insert;
DROP TRIGGER IF EXISTS trg_planner_runs_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_planner_runs_tenant_fk_insert;
DROP TABLE IF EXISTS dispatch_exceptions;
DROP TABLE IF EXISTS planned_stops;
DROP TABLE IF EXISTS planned_routes;
DROP TABLE IF EXISTS planner_runs;