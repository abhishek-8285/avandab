# 02 — UX Design: Dispatcher Workflow + Route Planner

> Screen- and flow-level UX for the dispatcher product. Reuses the existing
> Avandab UI system (server-rendered `html/template`, Tailwind, HTMX/SSE/Datastar
> islands, Leaflet map, 139 templates) and the Command Center (`/console`) wheel
> where possible. Reference patterns drawn from Samsara/Geotab/Verizon
> (`fleet-dispatch-route-research.md`). Wireframe-level, not pixel-perfect.

---

## 1. Design principles

1. **One screen, monitor-and-act.** The dispatcher works from a single map-
   centric board, not a maze of forms. (Samsara/Geotab map+list hybrid.)
2. **Orders are the unit.** No "create a load → convert to dispatch" ceremony.
   A booking/order *is* a routable stop. What a dispatcher manipulates is
   orders on a route.
3. **Plan before commit, always revertable.** Every planner change shows its
   impact (miles/duration/cost) and is committed or reverted explicitly
   (Samsara what-if).
4. **Status is per-stop and color-coded.** Single status model
   pending→accepted→en-route→on-site→done + missed/out-of-order, with
   plan-vs-actual ETA compared inline (Geotab).
5. **Exceptions are actions, not alarms.** A late/deviating stop leads the
   dispatcher to a ranked "fix it" action (reassign/reroute/mark missed) —
   from the same signals already in Command Center.
6. **Push, don't phone.** Dispatch changes sync to the driver app with a
   notification; SMS is fallback.

## 2. Information architecture

```
/dispatch                          ← NEW: dispatcher console (role 2)
├── Board (default)                ← map + work queue + stops
├── Planner                        ← route planning what-if
│    └── <Planner Run>             ← editable routes/stops
├── Runs (history)                 ← planned vs actual, "plan that corrects itself"
└── Exceptions                     ← flagged stops/vehicles needing action

(Reused, not rebuilt)
/console   Command Center (owner)  ← money strip, fleet strip, alert inbox
/trips     trip lifecycle          ← existing
/routes    corridor master + /routes/optimize legacy page
/mobile    driver app              ← extended with dispatch push + nav
```

Placement decision: a **separate `/dispatch`** surface (not an expansion of
`/console`) because the dispatcher persona (role 2) and the owner persona have
different jobs — the owner monitors money/compliance; the dispatcher runs
work. Both reuse the same fleet live-map + fleet-strip wheels. `/dispatch` is
role-gated (existing RBAC resource-permission middleware on "dispatch" resource).

## 3. Screen-by-screen

### S1 — Dispatch Board (`/dispatch`, default)
The core screen. Layout (mirrors Command Center's 3-column grid, which we
reuse for consistency):

```
┌─────────────┬──────────────────────────────┬──────────────────┐
│  QUEUE       │  LIVE MAP  (Leaflet)         │  STOPS PANEL      │
│  (left rail) │  vehicles + route polylines  │  selected route:  │
│  · Open      │  + unassigned stops          │  ordered stop list│
│    bookings  │                              │  per-stop status  │
│  · Unplanned │  ────────────────────────    │  color-coded,     │
│  · Planned   │  exception pins (late/dev.)  │  plan-vs-actual   │
│  · Dispatched│                              │  ETA              │
│  · In trip   │                              │                   │
└─────────────┴──────────────────────────────┴──────────────────┘
   exception banner / action bar (bottom):  [Detect plan] [Reoptimize] [Assign all]
```

- **Queue (left rail):** filterable buckets — *Open / Unplanned / Planned /
  Dispatched / In trip / Done*. Each row: order id, customer, pickup→drop,
  appointment window, urgency. Click → drop onto map.
- **Live map:** existing fleet-strip vehicles + planned route polylines +
  unassigned stop markers. Exception pins (late/deviated) float above.
- **Stops panel (right):** when a route is selected, its ordered stop list with
  per-stop status chip + planned-vs-actual ETA, mirrors Geotab.
- **Action bar:** primary actions surfaced: *Plan selected*, *Assign selected*,
  *Dispatch selected*, *Reoptimize*. (Reuses existing color/status system from
  fleet strip/alert inbox.)

States: loading → empty (no work: onboarding empty-state, "create a booking" →
  share booking) → work (board populated) → planning (modal/planner) →
  dispatched (offers pending) → live (in-trip). We reuse the existing
  Datastar/SSE partial-refresh pattern so the board updates without full
  reloads.

### S2 — Planner (what-if) — modal or full panel on `/dispatch`
Invoked by "Plan selected" on a selection of orders.

- **Left:** unassigned stop pool + selected orders' constraints (time window,
  demand/capacity, vehicle restriction, driver requirement).
- **Center:** candidate routes from the optimizer — each route as a card
  (vehicle, driver suggestion, stops count, miles, duration, cost) + map
  polylines + per-stop ETA chips.
- **Right:** route detail — reorderable stop list (drag), per-stop
  add/remove, "move to route" control, "unassign" to pool.
- **Header:** live **what-if KPI strip**: Total miles / hours / cost + drivers
  needed + unassigned stops; updates on every interaction before commit.
- **Command bar:** [Commit plan] [Revert] [Re-run optimizer] [Save as draft].
  Commit → goes to Assign step. Revert → returns to prior plan (what-if).

Interactions: drag-reorder stops; drag stop between routes; merge two routes
(tooltip shows merged impact); split a route; pin a "must be first/last" or
appointment stop; force vehicle/driver; mark a stop unserviceable → pool.

### S3 — Assign & Dispatch (part of planner flow)
Auto-suggest (ranked) or manual per route.

- Each route card expands **driver+vehicle suggestions**: ranked by
  qualification match, driving-hours remaining (MV Act), distance to first
  stop, current load/status, and vehicle capability. Shows why ranked.
  (Geotab/Samsara ranked-candidate pattern.)
- Manual fallback: pick driver (filter: eligible/available), pick vehicle
  (filter: capable/not blocked — reuse `IsMaintenanceBlocked` guard).
- **Dispatch:** [Send dispatch] → creates trips from routes + writes
  `dispatch_offers` per driver/vehicle; per-stop offer status feeds the board.
- Feedback: "1 route dispatched → John (driver) • MH12AB1234 • 8 stops" with
  offer state (offered/pending → accepted).

### S4 — Exception handling (on Board)
When telemetry/ETA/geofence flags a stop or vehicle:

- The stop/vehicle gets an exception pin + highlighted row + inline
  **"Fix it"** menu: *Reassign to nearest qualified driver* (shows ranked
  candidates with travel time + driving hours), *Reroute this route*, *Mark
  missed*, *Mark out-of-order*, *Pause*, *Call driver*.
- Choosing a fix shows its impact (delay delta, miles) — commit/revert.
- Applies → push to driver app → audit log (existing convention).

### S5 — Runs / Plan review ("plan that corrects itself")
- List of recent Planner Runs with plan-vs-actual (ETA, duration, miles) per
  route/stop from telemetry + trip history.
- **Biggest gaps** surfaced with a **recommended fix + one-click apply**
  (Samsara pattern) — e.g., split a chronically-late route, shift a start time,
  drop an over-loaded route's stops.
- Owner-visible (Command Center) as "route health."

## 4. Status model (single, reusable)

Per-stop status (shared across board + driver app + customer timeline `B9`):

```
pending → offered → accepted → en-route → on-site → done
                └→ rejected/expired → unassigned
          en-route → (deviation/late) → flagged (actionable)
                └→ out-of-order / missed
```

Color coding (reuse existing alert severity palette): gray=pending, blue=
accepted, amber=en-route, green=on-site/done, red=flagged/missed, dashed=
planned-but-not-yet-actual (plan-vs-actual opacity).

## 5. Mobile (driver app) changes (phase 2, small)
- Dispatch notification on assignment/change (in-app push primary, SMS fallback).
- "Next stop" card + duty-hours dial + toll/FASTag balance hint (P2).
- Turn-by-turn navigation reuse of existing Leaflet nav (P2 if traffic needed).
- Work confirmation already exists (ePOD sign/photo) — already shared by role 2.

## 6. Success = the flows feel like one system
The UX goal: a dispatcher never leaves `/dispatch` to do dispatch work. Every
other screen (trips, routes, alerts, driver) is reachable but planning,
assigning, dispatching, tracking and fixing happen on the board. That is the
single strongest differentiator vs the current scattered forms.
