# Dispatcher Workflow + Best Route Planner — Design Pack

> **"Another Samsara for India"** — design kickoff for extending Avandab/MVTMS
> with a dispatcher product and a best-route-planner. Product × UX × CTO.
> Deep-grounded in this codebase; reference from Samsara/Geotab/Motive/Verizon
> and primary routing-source research.

## The pack

| Doc | What it covers | Status |
| :-- | :-- | :-- |
| [`00-exec-summary.md`](00-exec-summary.md) | Why, codebase audit table, decisions to confirm, success criteria | Confirmed (D1–D5) |
| [`01-product-spec.md`](01-product-spec.md) | Personas, JTBD, workflows (daily dispatch / exceptions / plan-review), what "best" means, P0–P3 backlog, non-goals | Confirmed (D1–D5) |
| [`02-ux-design.md`](02-ux-design.md) | Info architecture, screen-by-screen (dispatch board / planner / assign / exceptions / runs), per-stop status model, mobile | Confirmed (D1–D5) |
| [`03-cto-architecture.md`](03-cto-architecture.md) | Core model (Planner Run → Route Plan → Stops), services, solver design, API, realtime, RBAC, phasing vs migration slots | Confirmed (D1–D5) |
| [`04-routing-source-recommendation.md`](04-routing-source-recommendation.md) | OSRM self-host now → GraphHopper/Valhalla → Mappls/Google; FASTag = post-processing | Confirmed (D1–D5) |

Related files produced this session:
- `fleet-dispatch-route-research.md` (workspace root) — competitive reference.
- `docs/12-ROUTING-DATA-SOURCE-RESEARCH.md` — full cited routing comparison
  (also resolves ROADMAP item C5's OSRM-posture question).

## Reading order
1. `00` — the short version + the decisions that need your call.
2. `01` → `02` → `03` — product, then UX, then CTO (they build on each other).
3. `04` — routing-source decision; also update `AVANDAB_ZERO_COST_ARCHITECTURE.md`
   per C5 when you adopt self-hosted OSRM.

## Decisions this pack asks you to confirm

**All five decisions confirmed by the owner** (2026-09-16, Asia/Calcutta).
D1, D3, D4 are **owner-CONFIRMED**; D2 and D5 stand as **confirmed-by-recommendation**
(default recommendations for the dispatcher persona + console scope, and
exceptions-as-events, were accepted without change).

- **D1** Dispatcher is a real persona with its own `/dispatch` console (role 2),
  reusing Command Center's map/fleet wheels — not a rewrite.
  **Owner-CONFIRMED — scope is NARROW**: compose existing pieces (dispatch
  workflow + board on existing trips/telemetry/geofence/optimizer), no full
  telematics/IoT pull-in.
- **D2** The route plan becomes a persisted, editable, promotable object
  (`Planner Run → Route Plan → Stops`) over the current disposable
  `route_optimization_jobs`. **Confirmed by recommendation.**
- **D3** Optimization becomes a real, constraint-enforcing VRP (time windows,
  capacity, driver hours) behind the existing `Optimizer` interface, returning
  geometry + per-stop ETA. **Owner-CONFIRMED — real VRP solver, now built
  (commit `0e6ae184`, `internal/route/optimizer/vrp.go`).**
- **D4** Routing: self-host OSRM now, phased to Mappls/Google later; FASTag as
  post-processing. **Owner-CONFIRMED — self-host OSRM, phased later.**
- **D5** Dispatcher exceptions are events from existing telemetry/geofence/ETA,
  not polls. **Confirmed by recommendation.**

## Implementation Status

**Design confirmed; groundwork committed.** Three cycles are in the tree so far:

| Commit | What landed |
| :-- | :-- |
| `9ab8ce61` | Dispatcher workflow schema renumbered to migration `00157` + tenant FK triggers |
| `400102a8` | sqlc query layer for the dispatcher workflow schema (`00157`) |
| `0e6ae184` | Real constraint-enforcing VRP solver (`internal/route/optimizer/vrp.go`, VRPMinCost) — D3 |

**Remaining (per `03-cto-architecture.md` phasing):**

- `internal/dispatch/application` layer — planner, tuner, assigner, exceptions, review.
- OSRM geometry + per-stop ETA extension to the optimizer output (D4 self-host, phased).
- `/dispatch` board API (`/api/v1/dispatch/...`) + server-rendered/SSE board UI.

## Open questions for the owner
1. Scope: narrow (dispatch + route planning composing existing pieces) or pull
   the full telematics/IoT product into the board now?
   **Resolved — NARROW, owner-confirmed (D1).**
2. Fleet profile: last-mile on-demand vs long-haul corridor vs both? (changes
   planner constraints + board layout)
3. First user: owner-operator (1–20 trucks) vs a real dispatch desk?
4. Live-traffic turn-by-turn a launch need, or v1 = matrix optimization with
   static speeds, traffic as phase-2 provider upgrade?

---
*This is a design brief that now gates build. Decisions D1–D5 are CONFIRMED
(2026-09-16); the "blocked on confirm" gate is lifted for these. Remaining
open questions (fleet profile, first user, live-traffic v1 scope) do not block
the committed groundwork and can be resolved as the build proceeds. Execute per
`03`'s phasing vs the migration ownership index (next free slots `00150+`).*
