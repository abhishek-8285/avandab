# Fleet Dispatch & Route Planning — Competitive Reference (Samsara / Motive / Geotab / Verizon Connect)

*Research basis: public product pages fetched directly (Samsara Routing & Dispatch, Samsara Commercial Navigation, Samsara Driver Assignment, Geotab Routing & Dispatching, Verizon Connect Distribution route planner). Zendesk/help centers (kb.samsara.com, helpcenter.gomotive.com, reveal-help.verizonconnect.com) are Cloudflare-gated; Motive's HubSpot product pages are JS-rendered and not crawlable, so Motive details below combine its public marketing with established product behavior. Verified vendor data is flagged with the source URL.*

---

## 1. Dispatcher Workflow

### Order → Dispatch
- **Orders feed route planning, and dispatch and navigation run on the same order/location/history data** — one change in any screen shows up in the rest. Samsara explicitly positions "routing + dispatch + commercial navigation" as one system, not three stitched tools. (https://www.samsara.com/products/telematics/routing/)
- Samsara: **"Orders assign themselves to the best available route"** — incoming orders are auto-matched to the best route; the planner adjusts and sees the impact on duration, miles, and cost *before committing* (a what-if / scenario loop). No separate "create load → convert to dispatch" ceremony; the order *is* the unit that gets routed.
- Geotab: dispatch units you can send are **a new job, a pickup/drop-off location, a zone, or an entire route** — dispatching is defined as sending any of these to a driver via MyGeotab. (https://www.geotab.com/fleet-management-solutions/routing-dispatching/)
- Geotab runs **two planning horizons** on the same engine: *Long-range planning* (economic optimization, balanced job distribution, shift patterns, minimized route overlap — reports 20–55% savings vs other models) and *Daily operations* (balanced daily job distribution, reduced miles, on-time arrivals, real-time response to change).

### Assigning drivers / vehicles
- Samsara **Driver Assignment** offers **9 methods**, from physical to software-only (https://www.samsara.com/products/workforce-management/driver-assignment/):
  - Driver App sign-in, QR Code, Camera ID, ID Card Reader, Driver ID Tokens, Static assignment, **Manual assignment**, API, Tachograph (EU).
- **Auto-assign / optimize**: the routing engine auto-assigns orders to the best route ("orders assign themselves"). Samsara also does **on-going auto-rebalancing**: "As accounts shift, find the best day to service each customer and balance workload" — recurring/weekly re-opt.
- Geotab adds **multi-resource routing & appointment scheduling** — balances workload across multiple resources/drivers and minimizes time waste; and **optimized reassignment suggestions** when a job needs rerouting/reassigning mid-day.
- (Motive provides a designated "Dispatcher" role with manual driver/vehicle assignment per trip plus configurable auto-assign rules by territory/qualification.)

### Dispatch modes
- **Single trip / point dispatch**: Geotab's "dispatch to a pickup/drop-off location."
- **Multi-stop route**: the core of Samsara routing (last-mile delivery, orders within one route) and Geotab multi-stop routing.
- **Live-load / next-stop on the road**: Geotab explicitly supports **allocating new jobs to drivers already on the road, highlighting the most suitable candidates by distance, travel time, and driving-time (HOS) status.** This is the "live reassignment" mode.
- **Zone dispatch**: Geotab can send a driver to an entire zone/custom territory.

### Notifications to drivers
- **The dominant pattern is in-app push, not SMS**: Samsara — "dispatchers can change a route in the office, and it automatically updates and **notifies the driver**, eliminating the need for phone calls and ensuring consistent ground truth." Route changes auto-sync to the Driver App. (https://www.samsara.com/products/telematics/commercial-navigation/ and /routing)
- Geotab Drive app: **instant dispatcher notifications** for assignments/alerts.
- Verizon: drivers get turn-by-turn commercial nav and custom routes pushed to the in-cab driver app.
- SMS/SMS-gateway is available in these platforms as an escalation fallback but isn't the primary UX — the product spec should treat push-to-app as primary and SMS as secondary for Indian fleets with spotty app usage.

### Live tracking, status, re-dispatch
- **Status model** (established fleet norm): *pending → accepted/acknowledged → en route → at stop/on-site → completed/delivered*, plus *missed stop / stop out of order*. Geotab explicitly exposes **"review missed stops or stops made out of order"** and **planned vs actual arrival time and stop duration**.
- **Mid-trip re-dispatch / reassignment**: Samsara's "manage exceptions in real time" and Geotab's "optimized reassignment suggestions" + live job allocation to the nearest suitable driver. Deviation/lateness triggers an exception the dispatcher resolves by reassigning or rerouting.
- **Bidirectional visibility**: drivers share ETA and navigation path with the dispatcher (Samsara Commercial Navigation), so dispatch doesn't call for updates.

### "Dispatch board" (queues / kanban / map)
- The canonical layout is **map-centric**: live vehicles over a map, with a **route/stop list panel** (planned order vs actual status per stop) beside it, and color/status coding per row. Samsara unifies route list + map + nav on the same data.
- Geotab MyGeotab = **live map of vehicles/technicians**, zones as overlays, with per-driver job lists.
- Verizon Reveal = **live map with geofences, geofence entry/exit alerts, route replay**, and configurable dashboards/reports.

### Metrics dispatchers watch
- On-time performance (planned vs actual arrival — Geotab), stop dwell/time-on-site (Samsara "Time on site" report flags unauthorized/inefficient activity), idle time, trip history, fleet utilization, speeding/harsh-driving, missed stops. (https://www.samsara.com/products/telematics/gps-fleet-tracking/)

---

## 2. Route Planner / Optimization

### What the optimizer solves
- **Multi-stop sequencing (TSP/VRP)**: order stops within/across routes to minimize miles & vehicles ("finish routes with fewer miles and vehicles" — Samsara).
- **Time windows & delivery appointments**: multiple appointment options to weigh (Geotab "Evaluate and choose from various appointment possibilities").
- **Capacity/weight/hazmat + vehicle restrictions**: Samsara optimizes by vehicle limitations (height, weight, hazmat) — not just duration.
- **Driver availability & duty hours (HOS)**: routing respects remaining drive time (Geotab considers "driving time status" when picking a candidate for live allocation).
- **Unassigned stops**: orders "assign themselves to the best available route"; balance workload when accounts shift.
- **Depot return / daily or period planning**: Samsara supports next-day *and* next-quarter planning; Geotab long-range planning produces territories/shift patterns (depot-implied).

### How the plan is presented
- **Route list + stop sequence + map polylines + ETA per stop + arrival windows**: standard across all four. Samsara shows plan-vs-actual continuously against Vehicle Gateway data.
- **"A plan that corrects itself"** (Samsara): continuously monitors each route vs. telematics; surfaces the **biggest plan-vs-actual gaps weekly with a recommended fix and one-click apply.** This closed feedback loop is worth copying whole.
- Presentation = route list (per-route ordered stops) + a map polyline per route + per-stop ETA/arrival window. Verizon communicates ETAs to customers directly; drivers navigate custom routes turn-by-turn.

### Manual override
- **Planner adjusts, sees impact on duration/miles/cost before committing** (Samsara "what-if" — commit/revert pattern).
- **Drag/reorder stops**, **merge routes**, **reoptimize** are the standard interactions; the spec should include an explicit "re-run optimizer on current selection" affordance. Geotab dispatchers can evaluate alternatives (multiple appointment options) before committing a reroute.

### Recurring/daily vs ad-hoc
- **Recurring/periodic**: Samsara weekly self-review + "find the best day to service each customer" (period/route-frequency optimization); Geotab long-range planning for balanced territories.
- **Daily/ad-hoc**: day-of ops with real-time reassignment, live job injection, and rerouting on traffic/closures (Samsara Commercial Navigation adjusts in real time for traffic/road closures).

### Constraints that matter most — mapped to India
- **Tolls & FASTag**: Samsara exposes the *pattern* as a company **policy "avoid tolls"** applied per vehicle/route (https://www.samsara.com/products/telematics/commercial-navigation/). For India this must become *FASTag-aware*: pick lanes/bridges with FASTag balance, estimate toll cost per edge, flag low balance, and include toll cost in route cost. No Western vendor does FASTag natively — this is a genuine local moat.
- **Weather & traffic**: Samsara reroutes live on real-time traffic and road closures; add India-specific monsoons/seasonal closures. Samsara's custom **Avoidance Zones** (auto-route away from high-risk/night-ban/low-clearance areas) map directly to India's night-entry bans, low bridges, and toll-gate congestion.
- **Driver duty hours**: Samsara overlays an **HOS dial on the navigation map** so drivers see remaining drive time while driving; Geotab uses driving-time status in candidate selection. In India substitute the FMCSA HOS with the **Motor Vehicles Act duty-hour rules (and platform-specific policies)**.
- **Vehicle restrictions**: height/weight/hazmat routing (Samsara) — needed for over-dimensional/container loads and weigh-bridges.

---

## 3. Notable UX patterns worth copying (short)

1. **Map + route-list hybrid dispatch board** — live vehicles on map, ordered stop list with per-stop status beside it; one screen for monitor-and-act.
2. **Status color coding & exception flags** — single color/status per stop (pending/accepted/en-route/on-site/delivered, + missed/out-of-order), with plan-vs-actual ETA compared per row (Geotab).
3. **"Plan that corrects itself"** — weekly auto-diff of plan vs actual telematics, with a recommended fix and *one-click apply* (Samsara). Strong product story.
4. **What-if commit/revert** — planner drags/reorders/stops, sees impact on duration/miles/cost before committing (Samsara).
5. **"Next stop" driver guidance with duty-hours overlay** — turn-by-turn embedded with a live HOS dial and fuel/toll awareness, so the driver never leaves the app (Samsara). Copy as "next stop + duty-hours + toll/FASTag overlay."
6. **Office→driver push without phone calls** — any dispatch change auto-syncs to the driver app with a notification; SMS as fallback (Samsara).
7. **Mid-route live reassignment with ranked candidates** — on lateness/deviation, surface the nearest/best-qualified driver with driving-hours status and let the dispatcher reassign in one action (Geotab).
8. **Geofence + zone-based dispatch & alerts** — enter/exit alerts, missed stops, dwell-time flags (Verizon/Geotab).
9. **Work-confirmation on driver app** — capture signature/photo proof at stop (Geotab Drive); important for POD in India.

---

## Source URLs
- Samsara — Routing & Dispatch: https://www.samsara.com/products/telematics/routing/
- Samsara — Commercial Navigation: https://www.samsara.com/products/telematics/commercial-navigation/
- Samsara — Driver Assignment: https://www.samsara.com/products/workforce-management/driver-assignment/
- Samsara — GPS Fleet Tracking: https://www.samsara.com/products/telematics/gps-fleet-tracking/
- Geotab — Routing & Dispatching: https://www.geotab.com/fleet-management-solutions/routing-dispatching/
- Verizon Connect — Delivery Route Planner (Distribution): https://www.verizonconnect.com/industries/distribution-delivery-route-planner/
- (Not crawlable this session: helpcenters of all four + Motive product pages — Cloudflare/JS-gated.)
