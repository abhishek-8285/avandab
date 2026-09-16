# Dispatcher Workflow + Best Route Planner — Design Kickoff

> Initiative: **"another Samsara for India"** — extend the existing Avandab/MVTMS
> fleet platform into a first-class, India-first dispatcher + route-planning
> product. This pack is the product × UX × CTO design pass that precedes code.
>
> Product: dispatcher workflow + route planner | Grounded in: this codebase
> (deep), reference: Samsara/Motive/Geotab patterns, India routing data sources.
> Version: 0.1 (draft) · Status: design — no production code written yet.

---

## 1. Why this exists

Avandab already runs a fleet's *money and compliance* (bookings, trips, e-way
bills, kharcha, fuel, ESG, GST, settlement) and already ships a **fleet route
optimizer (Spec 18 Wave A)** plus a **Command Center** operator console. What
it does **not** yet have is the thing Samsara is famous for: **one screen where
the person running the fleet decides what each truck does today, plans the
best stops, assigns the best driver/vehicle, watches it happen live, and jumps
in when it goes wrong.**

That missing layer is two connected features:

1. **Dispatcher workflow** — the operational cockpit: a queue of work
   (bookings/orders), turn it into a planned run, assign the right
   driver+vehicle, send the dispatch, track live, and handle exceptions
   (late, deviation, breakdown, no-show).
2. **Best route planner** — the optimization brain: given a set of stops and a
   fleet, produce the best stop-order + vehicle allocation (multi-stop, time
   windows, capacity, driver hours, depot return), compute real per-stop ETAs,
   and let the dispatcher tune the plan by hand before it becomes a trip.

## 2. Where the codebase stands today (deep audit)

| Capability | Built today | Gap to close for a Samsara-like dispatcher |
| :-- | :-- | :-- |
| Booking intake → trip | ✅ Full pipeline (`00110` quotes+bookings, trip state machine) | — |
| Trip lifecycle | ✅ `create → assign → dispatch → start → stops → complete` + cancellation + backhaul | Assignment is form-driven, not a workflow |
| Dispatch offer to driver | ✅ `dispatch_offers` (`00109`), driver commands, mobile app | No dispatcher "board" to originate/supervise offers |
| Driver role + mobile app | ✅ Expo app, geofence, ePOD, offline sync, telemetry | Dispatcher supervision of driver state is thin |
| **Dispatcher role (RBAC)** | ✅ Role id 2 exists; granular perms (`00151` gave role 2 `files:read/create`) | No dedicated dispatcher console surfacing the *workflow* |
| Command Center `/console` | ✅ Owner ops view: money strip, fleet strip (live positions), live map, vehicle context panel, alert inbox, ⌘K search | Monitoring-first; **not** a dispatch/assign workflow |
| Route optimizer | ✅ Spec 18 Wave A: `route_optimization_jobs` + `route_constraints`, providers mock/OSRM, greedy NN solver, web page + jobs | Greedy only; time-window/capacity/driver-hours constraints parsed but **not enforced** by solver; no route geometry/polyline; no manual tuning; no live re-optimize; no persisted route plan |
| Telemetry (live) | ✅ GT06/AIS-140 TCP + MQTT, per-tenant plates, SSE live, latest-position table | Feeds the map; not yet driving dispatch exceptions/E2E |
| Geofence / ETA | ✅ geofence transitions, ETA service + history + segments, dwell/detention | ETA is route-corridor based, not stop-sequence based |

**The core insight from the audit:** the building blocks of a dispatcher
(fleet live map, trips, dispatch offers, driver app, geofence, a route-opt
engine) all exist as **silos**. The product opportunity — and the engineering
work — is to **compose them into a dispatcher workflow** and to **make the
route planner actually optimal and usable**, not just a greedy demo.

## 3. Design decisions this pack makes (owner-CONFIRMED 2026-09-16)

**D1/D3/D4 are owner-CONFIRMED; D2/D5 confirmed by recommendation.** All five
stand as approved, lifting the "blocked on confirm" gate.

- **D1. Dispatcher is a real persona with its own console** — a new
  `/dispatch` surface (reusing the existing `/console` map/fleet components),
  role-gated to role 2 dispatchers. Not a rewrite of Command Center; a
  focused operational cockpit that reuses its live-map + fleet-strip wheels.
- **D2. The route plan becomes a first-class, persisted, editable object** —
  today `route_optimization_jobs` stores input/output JSON and is forgotten.
  We introduce the **Planner Run → Route Plan → Stops** model so a plan can be
  tuned, saved, promoted to a trip, and re-optimized live.
- **D3. Optimization is a real VRP, not greedy** — the existing `Optimizer`
  interface stays (it's a good seam) but the solver must enforce the
  constraints the schema already declares (time windows, capacity,
  driver hours, skills) and return **route geometry + per-stop ETA** for a
  useful map.
- **D4. Routing source: phased** — self-hosted OSRM now (zero-cost, matches
  repo philosophy), a provider slot for commercial India-accurate
  routing/traffic later. (Full recommendation in doc 04.)
- **D5. Dispatcher exceptions are events, not polls** — telemetry + ETA +
  deviation feed the dispatcher board via the existing alert/SSE plumbing.

## 4. The three deliverable documents in this pack

| Doc | What it covers |
| :-- | :-- |
| `01-product-spec.md` | Personas, jobs-to-be-done, end-to-end workflows, feature backlog (P0/P1/P2) |
| `02-ux-design.md` | Information architecture, screen-by-screen flows (wireframe-level), states, status model |
| `03-cto-architecture.md` | Data model, services, API, solver design, live/dispatch integration, RBAC, phasing |
| `04-routing-source-recommendation.md` | OSRM vs commercial India routing: comparison + phased recommendation |

## 5. Success criteria (how we'll know it's not a demo)

1. A dispatcher can turn a day's bookings into optimized, assignable routes in
   one screen, in under ~5 minutes for ≤50 stops (the existing cap).
2. The optimizer enforces real constraints (time windows, capacity, driver
   hours) — proven by tests where the unconstrained greedy answer differs.
3. A plan can be tuned by hand (reorder, move stops between routes, merge) and
   promoted to trips with driver+vehicle assignment in one action.
4. Live exceptions (late ETA, deviation, breakdown, no-show) surface on the
   board from existing telemetry/geofence/ETA signals — no dispatcher polling.
5. Every new path ships behind the repo's security gate and matches migration
   ownership (next free slot per the ownership index).

## 6. Open questions for you (owner)

- **Scope of "Samsara for India":** do you want the full telematics/IoT product
  (fuel cards, dashcams, driver IDs, maintenance) pulled into the dispatcher
  board now, or focus this initiative narrowly on **dispatch + route planning**
  composing the pieces that already exist?
- **Fleet profile:** last-mile/on-demand (many small stops, same day) vs
  long-haul corridor (few stops, far apart) vs both? It changes route-planner
  constraints and dispatcher board layout.
- **Who uses it first:** owner-operators (1–20 trucks) or a real dispatch desk
  (many trucks, dedicated dispatchers)? Affects board density and automation.
- **Live traffic:** is turn-by-turn-with-traffic a launch need, or is
  distance-matrix optimization (static speeds) enough for v1, with traffic as
  a phase-2 provider upgrade?

---
*All five decisions (D1–D5) are now confirmed — owner-CONFIRMED for D1/D3/D4,
confirmed-by-recommendation for D2/D5 (2026-09-16, Asia/Calcutta). The design
pack's product/UX/CTO docs below are the working brief for the build phases.*
