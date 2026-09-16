# 03 — CTO Architecture: Dispatcher Workflow + Best Route Planner

> Technical design. Grounded in the current code (paths and migrations cited
> inline). Extends, composes and only minimally changes existing seams.
> **Decisions D1–D5 owner-confirmed (2026-09-16)** — this doc is the working
> brief for the build phases; the "blocked on confirm" gate is lifted.

---

## 1. Goals & constraints

- Compose **existing** silos, don't rebuild: trips lifecycle, `dispatch_offers`
  (`00109`), fleet live map + strips (Command Center `console.go`), telemetry
  ingest, geofence, ETA, route optimizer (`Spec 18 Wave A`, `00066`).
- Reuse the existing **`Optimizer` interface seam** (`internal/route/optimizer/
  provider.go`) — the API layer never branches on provider.
- Follow the repo's hard rules: append-only migrations (next free slot per
  `docs/tech-specs/00-migration-ownership-index.md`, head `00149`, next `00150+`),
  multi-tenancy everywhere (`TenantIDFromContext`), security gate + tests
  (ratchet law), no fake fixes.
- Stay on the current stack: pure-Go + Chi + SQLite/PG + server-rendered
  templates + HTMX/SSE/Datastar + Leaflet + Expo mobile.

## 2. The core model change: a route plan is a first-class, editable object

Today `route_optimization_jobs` (`00066`) stores `input_json`/`result_json`
and is effectively disposable after solve. For a dispatcher, the plan must be
**persisted, tunable, promotable to trips, and re-optimizable**. Add a
lightweight plan layer over the existing job (do **not** rewrite `00066`):

```
Planner Run ──► Route Plan ──► Route (per vehicle) ──► Stops (ordered)
   │                │                │                    │
   │ (input_json)   │ (editable)     │ (vehicle/driver)   │ (status, ETA, actual)
   └── reuses route_optimization_jobs
```

Design choice: keep `route_optimization_jobs` as the **solve artifact**. Add
new tables `planner_runs`, `planned_routes`, `planned_stops` that reference trip
and booking ids and store the *editable, live* plan. This keeps the optimizer a
pure function (jobs stay as input/output + provider + timing — audit value) and
gives the dispatcher a mutable working set. Promote (`planned_route` → `trip`)
marks the linkage and moves ownership to the trip lifecycle.

### Proposed schema sketch (to be slotted, each ≤1–2 migrations)

```sql
-- planner_runs: a dispatch planning session (WF-A step 2)
CREATE TABLE planner_runs (
  id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL,
  status TEXT NOT NULL CHECK(status IN ('draft','planned','assigned','dispatched','done','cancelled')),
  job_id TEXT REFERENCES route_optimization_jobs(id),   -- solve artifact link
  source TEXT NOT NULL DEFAULT 'orders',                -- orders | adhoc
  created_by TEXT, created_at DATETIME NOT NULL DEFAULT (CURRENT_TIMESTAMP),
  -- KPI of last what-if (miles / hours / cost / unassigned), for the board strip
  kpi_json TEXT, committed_at DATETIME
);

-- planned_routes: a vehicle's ordered run inside a run
CREATE TABLE planned_routes (
  id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, run_id TEXT NOT NULL REFERENCES planner_runs(id) ON DELETE CASCADE,
  seq INT NOT NULL, vehicle_id TEXT, driver_id TEXT,
  -- suggested assignment (what ranked) vs confirmed (assign)
  suggested_driver_id TEXT, suggested_vehicle_id TEXT,
  total_km REAL, total_min REAL, toll_cost_est REAL,        -- P2 FASTag
  status TEXT NOT NULL DEFAULT 'planned'
);

-- planned_stops: one stop per booking/trip leg, ordered
CREATE TABLE planned_stops (
  id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, route_id TEXT NOT NULL REFERENCES planned_routes(id) ON DELETE CASCADE,
  seq INT NOT NULL, booking_id TEXT, source_type TEXT NOT NULL CHECK(source_type IN ('pickup','dropoff','waypoint','backhaul')),
  address TEXT NOT NULL, lat REAL NOT NULL, lng REAL NOT NULL,
  time_window_start DATETIME, time_window_end DATETIME,
  demand REAL, skills TEXT,
  -- status mirrors UX §4; actual filled from telemetry/geofence/trip
  status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','offered','accepted','en_route','on_site','done','missed','out_of_order','unassigned')),
  planned_eta DATETIME, actual_eta DATETIME, planned_duration_min REAL, actual_duration_min REAL,
  created_at DATETIME NOT NULL DEFAULT (CURRENT_TIMESTAMP)
);

-- exceptions: surfaced dispatch actions (WF-B), feed the board + alerts
CREATE TABLE dispatch_exceptions (
  id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, stop_id TEXT, trip_id TEXT, vehicle_id TEXT,
  kind TEXT NOT NULL CHECK(kind IN ('late','deviation','dwell','breakdown','no_show','missed','sto_out_of_order')),
  status TEXT NOT NULL DEFAULT 'open' CHECK(status IN ('open','acting','resolved','dismissed')),
  detail_json TEXT, created_at DATETIME NOT NULL DEFAULT (CURRENT_TIMESTAMP)
);
```

> Numbers come from existing code: `route_optimization_jobs` + `route_constraints`
> (`00066`), `dispatch_offers` (`00109`), `trips` statuses
> (`assigned/started/reached_pickup/in_transit` — `console.go`), `dispatch_exceptions`
> from `vehicle_breakdown` ops alerts (B1), geofence deviation, ETA history.

## 3. Services (Go)

Add an `internal/dispatch/` package (mirror the clean layout used elsewhere:
`domain/` + `application/` + `infrastructure/` + `presentation/`), and grow
`internal/route/optimizer`.

- **`dispatch/application/planner.go`** — PlannerRun service: create run from
  orders, call optimizer, persist `planned_routes/stops`, compute
  what-if KPI, commit/revert.
- **`dispatch/application/tuner.go`** — manual edit ops: reorder, move stop
  between routes, merge/split routes, unassign; always recompute what-if KPI
  (pure function over the mutable plan).
- **`dispatch/application/assigner.go`** — ranked driver+vehicle suggestions
  (qualification, MV-Act hours remaining, distance, load, `IsMaintenanceBlocked`
  guard — reuse the `work_orders` guard from `00123`); confirm → writes trip
  + `dispatch_offers`.
- **`dispatch/application/exceptions.go`** — read signals (ETA slip, geofence
  deviation, dwell, breakdown, no-show) → open/close `dispatch_exceptions` →
  expose ranked remediation actions (reassign/reroute/mark missed). Reuses
  existing `eta.EtaService`, geofence events, `ops_alerts`.
- **`dispatch/application/review.go`** — plan-vs-actual (planned vs
  telemetry/trip actual) + recommended-fix + one-click-apply ("plan that
  corrects itself").
- **`route/optimizer` (grow)** — see §4.

Services that already exist and are consumed, not duplicated: `trip`
(`assign_driver/assign_vehicle/create/schedule`), `eta.EtaService`, geofence,
`repo.AlertRepository` (Command Center inbox reuse), `service.PNLService`
(route cost KPIs), `fastag` (P2 tolls), `booking` (orders as stops).

## 4. Route planner — solver design (the real engineering)

**Keep the `Optimizer` interface** (`provider.go`). Its contract is right; the
gap is *what the current implementations do*: greedy NN over OSRM Table, no
constraint enforcement, no geometry, `Validate` caps at 50 shipments. Grow:

- **`VRPMinCost` real solver** (P1): a proper VRP solver implementation of
  `Optimizer` — nearest-neighbor + 2-opt/Lin-Kernighan improvement + constraint
  filtering (time windows, capacity, vehicle restrictions, driver hours,
  depot/corridor return). Deterministic (required by the interface contract).
  Route **cost = distance + duration + toll estimate (P2)** — matches the
  existing `TotalCost` field.
- **Route geometry + per-stop ETA**: the optimizer's `OptimizationOutput` today
  has legs (shipment/seq/distance/duration) but **no geometry or per-stop
  arrival time**. Extend the output shape (add `geometry` polyline per route
  and `eta` per leg) so the board can draw real paths and show per-stop
  windows. Backends: OSRM `/route` returns geometry + each-leg duration;
  solvers must propagate it. This is additive to the output struct (keep the
  old fields for compat).
- **Provider tiering**: mock (dev/test/fallback) → OSRM self-host (default,
  zero-cost) → commercial (P2+, doc 04). No API-layer provider branching
  (interface preserved).
- **Async solving** (P1/P3): current `Optimize` handler solves *synchronously*
  in the request path (`routes.go`), fine for mock/OSRM ≤50. For larger runs,
  move solve onto the existing leadered cron/outbox path and poll via the
  existing job-status endpoint. Not required for v1.

### API surface (new, spec-compliant under `/api/v1`)

Reuse the handler conventions in `internal/handlers/routes.go` / `console.go`
(RBAC via `middleware.ResourcePermission`, JSON + Datastar fragment twins).

```
POST   /api/v1/dispatch/runs            create planner run from orders
GET    /api/v1/dispatch/runs            list runs (board)
GET    /api/v1/dispatch/runs/{id}       run + routes + stops + KPI
POST   /api/v1/dispatch/runs/{id}/plan  solve (calls optimizer, persists plan)
PATCH  /api/v1/dispatch/runs/{id}       manual tune (reorder/move/merge/unassign)
POST   /api/v1/dispatch/runs/{id}/assign{ ,/route}/{routeId}   suggest/confirm assignment
POST   /api/v1/dispatch/runs/{id}/dispatch   commit → trips + dispatch_offers
GET    /api/v1/dispatch/exceptions       open exceptions + ranked fixes
POST   /api/v1/dispatch/exceptions/{id}/resolve
POST   /api/v1/dispatch/review/fix       "plan that corrects itself" apply
```

Web twins under `/dispatch` (server-rendered + SSE/HTMX partials) mirror these.

## 5. Live / realtime integration (P0.5)

- **Stop status** from existing sources, not new polling: geofence enter/exit
  (existing), trip state machine (`console.go` already joins `trips` statuses),
  ETA history/segments, telemetry latest-position. A small
  `dispatch/infrastructure/status_worker.go` (leadered cron, existing pattern)
  folds these into `planned_stops.status` + `dispatch_exceptions`.
- **Board updates** reuse the existing SSE/Datastar partial-refresh path
  (already proven for the Command Center live map). No new realtime stack.
- **Driver push** reuses `dispatch_offers` + `driver_commands` (`00109`) + the
  existing push-token/notification path (mobile already has notifications).

## 6. RBAC / multi-tenancy

- New `dispatch` resource with `read`/`create`/`update` permissions; grant to
  **role 2 (dispatcher)** + org_admin, consistent with `00151` precedent.
  Every query tenant-scoped via `shared.TenantIDFromContext` (no hardcoded
  tenant — hard rule).
- Audit: reuse `audit_logs` + the `writeAuditLog` convention (`console.go`).

## 7. Phasing vs migration slote

| Phase | Deliverables | Migrations |
| :-- | :-- | :-- |
| **P0** | Board UI reusing map/fleet strips; Planner Run → solve → render; manual tune (what-if); assign + origin offers; stop status + exceptions surfacing from existing signals | `planner_runs`/`planned_routes`/`planned_stops` (1–2), `dispatch_exceptions` (1) |
| **P1** | Real VRP solver + constraint enforcement; geometry/ETA in output; live re-assignment (ranked); plan-vs-actual review + one-click fix | additive columns only (geometry JSON), or none |
| **P2** | FASTag-aware routing (toll edges/cost, low-balance), avoidance zones (geofence → routing bans), MV-Act duty-hours | toll/zone linkage tables (1–2) |
| **P3** | Owner analytics (route savings/utilization/on-time); async solving; matrix caching | none / index |

Follow `00-migration-ownership-index.md` — reserve the next free slots
(`00150+`) before authoring; one migration per concern; never edit an existing
migration (append-only).

## 8. Key risks & mitigations

- **Real VRP is hard to get right / test.** Mitigate: keep a deterministic
  greedy baseline as fallback + a documented optimal-vs-greedy divergence test
  (ratchet law: prove the solver honors a binding time window or capacity).
- **Routing geometry from OSRM adds latency/state.** Mitigate: cache polylines
  per (origin,dest) in an existing cache; compute per-leg ETA at plan time and
  refresh live from `eta.EtaService`.
- **State split** (jobs vs runs) could diverge. Mitigate: `job_id` FK + a
  single `commit` path; runs reuse job input/result as the solve source of truth.
- **Scope creep into full telematics/IoT.** Mitigate: P0 composes existing
  telemetry only; new hardware/analytics is out of scope (exec summary non-goal).
- **Approval changed to "never" in this session** — mutating dispatch tools
  (assign/dispatch) must respect the agent gate (`AGENT_REQUIRE_APPROVAL`)
  rather than a session override. Keep human-in-the-loop for auto-dispatch.

## 9. What we do NOT build (bounding scope)

- No new telemetry ingest / devices / dashcams.
- No full turn-by-turn commercial nav in v1 (route geometry + ETAs are v1;
  traffic-directed nav = doc 04 provider decision).
- No billing/finance namespace changes (reuse trips/fuel/settlement).
- No rewrite of `route_optimization_jobs` (`00066`) — additive only.
