# 01 — Product Spec: Dispatcher Workflow + Best Route Planner

> Product intent for extending Avandab/MVTMS into an India-first dispatcher
> product. Written as a working brief for the build phase. Grounded in a deep
> audit of this codebase, with competitive reference from Samsara / Geotab /
> Verizon (see `00-exec-summary.md` §2 for the audit table, and the reference
> file `fleet-dispatch-route-research.md` in the workspace root).

---

## 1. Who we're building for

Avandab already models the fleet's people and money: bookings, trips, drivers,
vehicles, dispatch offers, kharcha, e-way bills, fuel, ESG. The **dispatcher
role exists in RBAC (role id 2)** and was recently given `files:read/create`
(`00151`) so dispatchers can handle ePOD photos — proof that a real human
operates trips daily. The product persona is that human, plus the owner who
hires them.

| Persona | Who they are | Core need | Current tooling | 
| :-- | :-- | :-- | :-- |
| **Dispatcher** (primary) | Runs the fleet's trips day to day; ~10–100 vehicles | Decide what each truck does today, plan stops, assign the best driver+vehicle, send the dispatch, watch live, fix exceptions | Ops → trips forms, bookings board, Command Center (monitor-only), driver app for offers |
| **Owner / Ops Manager** | Owns the P&L; reviews utilization, on-time, cost | Trust dispatch is optimal; see route savings, missed stops, dwell | Command Center (money strip, fleet strip, alert inbox) |
| **Driver** (mobile) | On the road; already in the Expo app | Get assigned work, navigate, confirm stops (ePOD), report issues | Driver app: trips, geofence, ePOD, chat, telemetry — extend with dispatch notifications + nav |

## 2. Jobs to be done (JTBD)

For each, we mark existing support, so the build focuses on the gap.

| # | Job | Built today | Gap |
| :-- | :-- | :-- | :-- |
| J1 | See today's work in one place (bookings/orders/loads) | Bookings board + trips list, separate | No unified **work queue** tied to planning |
| J2 | Plan the best run for a set of orders | Route optimizer page (greedy, mock/OSRM) | Not integrated, not editable, no geometry/ETA |
| J3 | Assign the right driver + vehicle to each planned route | Manual trip assign + dispatch offers (`00109`), role gates | No ranking/suggestion, no plan→assign one-action |
| J4 | Send the dispatch and get acceptance | `dispatch_offers` (`offered/accepted/rejected/expired/cancelled`), driver commands | Offers exist but are not originated from a dispatcher board |
| J5 | Track live status + ETA per stop | Telemetry live map, ETA service, geofence, Command Center | Status per **stop**, plan-vs-actual, exception surfacing |
| J6 | Handle exceptions (late, deviation, breakdown, no-show) | Alerts inbox (Command Center), geofence deviation | Exceptions are *monitoring* alerts, not *dispatch-actions* (reassign/reroute) |
| J7 | Re-dispatch mid-route with the best candidate | Backhaul offers (B7) on delivery | No general live-reassignment with ranked candidates |
| J8 | Review how good the plans were (plan-vs-actual) | Trip reports, gate register | No plan-vs-actual diffs / "plan that corrects itself" |

## 3. End-to-end workflows

### WF-A — Daily dispatch (the flagship flow)
1. **Morning work intake.** Dispatcher opens `/dispatch`. Board shows: open
   bookings/orders (work queue) + fleet live map + status strips (existing
   fleet strip components reused).
2. **Plan.** Select orders → **"Plan routes"**. The optimizer builds
   candidate routes (multi-stop, respecting time windows/capacity/driving
   hours) → shown as routes + stop list + map polylines + per-stop ETA
   (**what-if**: impact on duration/miles/cost before committing).
3. **Tune.** Drag/reorder stops, move stops between routes, merge routes,
   unassign a stop (→ "unassigned stop" pool), force a driver/vehicle, mark
   "must be first/last" or a required appointment window.
4. **Assign.** Auto-suggest the best driver+vehicle per route (by
   qualification, driving-hours remaining, distance, current load) OR assign
   manually. One action per route (or "assign all").
5. **Dispatch.** Commit plan → trips created from routes → `dispatch_offers`
   pushed to driver apps (in-app push primary, SMS fallback). Dispatcher sees
   `offered → accepted → en route → on-site → done` per stop.
6. **Track & close.** Live stop status drives the board. Completed stops
   capture ePOD (existing). Trip closes via existing lifecycle.

### WF-B — Exception handling / live re-dispatch
- Triggered by existing signals: ETA slip (late), geofence deviation, dwell
  timeout, breakdown (`vehicle_breakdown` ops alert), driver no-show.
- Board surfaces the exception with a **ranked action menu**: reassign stop to
  nearest qualified driver (ranked by distance + travel time + driving-hours),
  reroute the route, mark stop missed/out-of-order, or pause.
- One-click applies; pushed to driver app; audit-logged (existing convention).

### WF-C — Plan review ("plan that corrects itself")
- Periodic (weekly / daily) job compares **planned vs actual** per route/stop
  (ETA, duration, miles) from telemetry + trip history.
- Surfaces the biggest gaps with a recommended fix and **one-click apply**
  (e.g., "this route is chronically late: try splitting at X / starting 1h
  earlier / it needs 3 stops fewer"). Feeds dispatcher + owner.

## 4. Route Planner — what "best" means

The planner must optimize across the constraints Avandab already models, and
the ones India uniquely needs:

| Constraint | Source today | Optimizer must enforce |
| :-- | :-- | :-- |
| Multi-stop order (TSP/VRP) | `route_optimization_jobs` + greedy solver | Real VRP (see doc 03); fewer miles & vehicles |
| Time windows / appointments | `route_constraints` (`time_window`), stops | Enforce (solver must reject/route around) |
| Capacity / weight | `optimizer.Vehicle.Capacity`, `Shipment.Demand` | Enforce |
| Vehicle restrictions (height/weight/hazmat) | vehicle master | Add per-vehicle restriction → route around |
| Driver availability / duty hours | driver master, trips | **MV Act duty-hours** (replace FMCSA HOS) |
| Unassigned stops | N/A | Expose + rebalance; "orders assign themselves" |
| **Tolls / FASTag** (India moat) | `fastag_tags`, `fastag_transactions` , kharcha cross-check | **FASTag-aware**: toll cost per edge, lane/bridge choice, low-balance flag, toll cost in route cost |
| **Avoidance zones** (night-ban, low-bridge, monsoon, toll congestion) | geofence (`geofence_edit`/`geofence_list`) | Route away from high-risk / banned zones |
| Depot return / corridors | routes master (origin→destination corridors) | Respect corridor/reverse-distance semantics where relevant |

**Acceptance:** the solver's answer must differ from the naive greedy answer
when constraints bind, and this must be proven by tests (per the repo's
ratchet law), not by vibes.

## 5. Backlog (phased)

### P0 — Dispatcher board v1 (compose existing pieces, no new routing math)
- `P0.1` `/dispatch` page: work queue + fleet live map + stop-status strips
  (reuse Command Center map/fleet components and templates).
- `P0.2` Turn a selected set of bookings/orders into a **Planner Run** and call
  the existing optimizer; render routes (list + map + per-stop ETA).
- `P0.3` Manual tuning: reorder stops, move between routes, merge, unassign;
  **what-if impact** (miles/duration/cost) before commit.
- `P0.4` Plan → trips with driver+vehicle assignment; origin `dispatch_offers`
  from the board; per-stop offer status on the board.
- `P0.5` Stop-status live from existing telemetry/geofence/ETA; plan-vs-actual
  per stop; exception flags surfaced as actions.

### P1 — Real VRP + live re-dispatch
- `P1.1` Enforce time windows, capacity, vehicle restrictions, driver hours in
  the solver (extend the existing `Optimizer` interface impls); return route
  geometry (polyline) + per-stop ETA/window.
- `P1.2` Live reassignment: ranked candidate selection (distance + travel time
  + driving-hours) + one-click reassign (Geotab pattern).
- `P1.3` "Plan that corrects itself": planned-vs-actual diff + recommended fix
  + one-click apply (Samsara pattern).

### P2 — India moat
- `P2.1` **FASTag-aware routing**: toll edges + cost, lane/bridge choice, low-
  balance flag; integrate `fastag_tags/transactions`.
- `P2.2` **Avoidance zones / MV Act duty-hours**: geofence-based routing bans
  (night-entry, low-bridge, monsoon), driver-hours dial overlay on driver app.
- `P2.3` Recurring/territory planning: "find the best day to service each
  customer" + auto-rebalance (Samsara next-quarter/Geotab long-range).

### P3 — polish (lower priority)
- Owner analytics (route savings, utilization, on-time) on the Command Center.
- Multi-tenancy scale (matrix caching, async solver off the request path).

## 6. Non-goals for v1 (call these out)
- No new hardware/IoT telematics push — reuse the existing TCP/MQTT ingest.
- No full commercial turn-by-turn nav in v1 unless traffic is a launch need
  (decision D open — see exec summary). Route geometry + ETAs are v1; live
  traffic-directed nav is a provider upgrade.
- No new billing namespace: reuse trips/fuel/settlement for route economics.
- Keep the dispatcher surface in the same server-rendered + HTMX/SSE + Leaflet
  stack (matches the existing 139-template architecture and mobile app).

## 7. User-meaningful success criteria (mirrors exec summary §5)
1. ~5 minutes from "today's orders" to "optimized, assigned, dispatched"
   for ≤50 stops (existing cap).
2. Optimizer provably honors time-window/capacity/driver-hour constraints
   (tests show greedy-vs-optimal divergence).
3. Plans are tunable by hand and promotable to trips in one action.
4. Exceptions (late/deviate/breakdown/no-show) surface on the board from
   existing signals — no polling.
5. Every path passes the repo security gate; new schema ships per the
   migration ownership index (next free slot `00150+`).
