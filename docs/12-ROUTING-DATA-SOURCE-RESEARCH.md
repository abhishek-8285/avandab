# 12 — Routing Data Source: Decision Research (India Fleet)

> **Purpose.** Decide the routing + turn-by-turn + distance-matrix data source for an
> early-stage India fleet startup. The existing codebase already has an **OSRM client**
> that calls the OSRM **Table** API for distance matrices and does greedy
> **nearest-neighbour** assignment (single-vehicle routing / VRP baseline). A "best route
> planner" and turn-by-turn engine is being built on top.
>
> **Decision lens.** Repo philosophy is **zero-cost architecture** (see
> `docs/AVANDAB_ZERO_COST_ARCHITECTURE.md`; C5 in `docs/ROADMAP.md` asks to reconcile the
> OSRM posture). Goal: **zero cost first, paid later**, without painting into a corner.
>
> Every factual claim is traced to a primary source (official docs / vendor pages) cited
> inline. Pricing figures are the vendors' published pay-as-you-go rates; **verify at
> sign-up** — they change.

---

## 1. The candidates at a glance

| Option | Self-hosted? | Free tier | Live traffic | Matrix | VRP / optimisation | Turn-by-turn | India fit |
|---|---|---|---|---|---|---|---|
| **OSRM** | Yes | Yes (software free) | No (static profiles) | `/table` | `/trip` = TSP only (single route) | `/route` with `steps=true` (maneuvers/intersections) | Good OSM coverage; address quality = whatever OSM has |
| **GraphHopper** | Yes | Yes (FOSS, self-host) | Optional/No | `/matrix` | `/optimize` = real VRP (time windows, capacity, breaks) | `/route` full guidance | Good; same OSM street caveat |
| **Valhalla** | Yes | Yes (MIT) | Optional (live + predicted) | `/matrix` (time+distance) | `/optimized` = TSP; flexible auto/truck costing | `/route` strong narrative (multilingual incl. Indian locales) | Good; flexible costing |
| **Google Routes / Directions / Distance Matrix** | No | ~$90/mo free credit then paid† | **Yes (live)** | Compute Route Matrix (up to 625 elements) | No raw VRP endpoint (Route Optimisation via partners) | Excellent (voice / Navigation SDK) | Excellent coverage & accuracy |
| **MapmyIndia / Mappls** | No (API/SDK) | Freemium / plans | **Yes (traffic)** | Matrix API | Optimisation APIs & SDKs (route planning/optimisation) | Full Navigation SDK + turn-by-turn | **Homegrown India maps** — best for pin code, landmarks, last-mile |

---

## 2. What each one actually gives you

### 2.1 OSRM — self-hosted
Primary source: [OSRM HTTP API docs (v5.x)](https://project-osrm.org/docs/v5.24.0/api/)

Services: `route`, `nearest`, `table`, `match`, `trip`, `tile`.

- **Route** — polyline geometry (precision-5 `polyline`, `polyline6`, or GeoJSON), with
  `steps=true` returning turn-by-turn: `maneuver` (turn/merge/roundabout/off-ramp…),
  `modifier` (left/right/straight…), `intersections` (bearings, lanes, entry flags),
  `name`/`ref`, `driving_side`. Enough for a custom navigation UI, but street names are
  whatever OSM has in the `name` tag (sparse for Indian last-mile).
- **Table** — distance **and** duration matrices (row-major `durations[i][j]` /
  `distances[i][j]`), `sources`/`destinations` projection, `fallback_speed` for
  disconnected pairs. This is exactly what the existing client calls.
- **Trip (TSP)** — solves the Traveling Salesman Problem via a **greedy farthest-insertion
  heuristic** for ≥10 waypoints, brute force below. Supports `roundtrip`, `source=first`,
  `destination=last`. **Single route only** — no multiple vehicles, no time windows, no
  capacity, no breaks. That is the ceiling of what OSRM can do for VRP.
- **Nearest** — snaps a coordinate to the network.

**Limits / caveats.**
- Speed profiles are **static** (set by a Lua profile at `osrm-extract` time). No live
  traffic, no historical prediction.
- Public demo server (`router.project-osrm.org`) is **low-volume / non-production**
  (fair-use rate limits) — not safe for a production fleet on India data.
- Road/address quality is exactly OSM's. Good on highways/arterials; weak on unmapped
  last-mile lanes, gated colonies, non-latin street names.

### 2.2 GraphHopper — self-hostable, real VRP
Primary source: [GraphHopper Directions API (OpenAPI)](https://docs.graphhopper.com/openapi)

- `/route` — full turn-by-turn guidance.
- `/matrix` — distance/time matrix endpoint.
- `/optimize` — **true vehicle-routing (VRP)**: vehicles, services with **time windows**,
  **capacity** (load/delivery), breaks/rests, round trips, depot (start/end) consistency.
  This is the key difference vs OSRM: **multi-vehicle optimisation with constraints**, not
  a single-route TSP.
- Self-hostable open-source GraphHopper core; hosted Directions API is commercial.
- Same OSM street-quality caveat, but instructions/geocoding are richer than OSRM's.

**OSRM vs GraphHopper for VRP:** OSRM `/trip` optimises one route; you build VRP on top of
its matrix. GraphHopper `/optimize` does multi-vehicle time-window + capacity optimisation
for you. If fleet needs "many vehicles, delivery windows, load capacity" → **GraphHopper
`/optimize` wins**. If need is "cheapest single route, you already have the VRP loop" →
OSRM is sufficient.

### 2.3 Valhalla — self-hostable, flexible costing
Primary source: [Valhalla docs](https://valhalla.github.io/valhalla/)

- Open source (MIT). Route + **matrix (time & distance)** + isochrone + elevation +
  map-matching + **optimised (TSP)**.
- **Dynamic runtime costing** via plugin architecture: **auto, truck, bus, bicycle,
  pedestrian**, custom costings. Configurable per-vehicle weight/height/axles/hazmat, and
  toll-aware options — attractive for freight.
- **Historical speed** data; **live traffic incidents** supported as optional tiled input.
- Strong turn-by-turn narrative generator (Odin), **multilingual** with a locale set that
  includes Indian locales/languages — better localised instruction text than OSRM.
- Tiled data structure = smaller memory footprint + regional extracts (easier to scale than
  OSRM's monolithic CH build).

**OSRM vs Valhalla:** OSRM `/table` is more battle-tested / faster per-query for pure
matrices; Valhalla wins when you need **truck costing, isochrones, or live-traffic later**
without switching engines.

### 2.4 Google Routes API / Directions / Distance Matrix — commercial
Primary sources:
- [Google Maps Platform pricing & billing (Global)](https://developers.google.com/maps/billing-and-pricing/pricing?hl=en)
- [Google Maps Platform pricing — India](https://developers.google.com/maps/billing-and-pricing/pricing-india?hl=en)
- [Routes API coverage](https://developers.google.com/maps/documentation/routes/coverage)
- [Compute Routes / Compute Route Matrix product page](https://mapsplatform.google.com/maps-products/routes/)

- **Compute Routes** — directions for driving/2-wheel/transit/walking; live-traffic-based
  ETAs, toll-disclosure fields, polyline + full turn-by-turn.
- **Compute Route Matrix** — distance + duration matrix, **capped at 625 route elements
  (origins × destinations) per request**. Great for 1×N "depot → many stops"; a large N×N
  VRP matrix must be chunked (each chunk billed).
- **Live traffic** is Google's biggest differentiator vs all self-hosted engines.
- **Route Optimisation** (multi-vehicle, flexible) exists as a Maps Platform offering via
  partners / AI-agent path, not a raw "VRP endpoint" like GraphHopper `/optimize`.

**Cost (standard published pay-as-you-go; verify at sign-up)†:**
- Directions (basic): ~$5 / 1,000 requests.
- Distance Matrix (basic): ~$5 / 1,000 elements (element = origin×destination cell).
- Routes Advanced / Compute Routes field components & Compute Route Matrix: higher,
  billed per element.
- ~$90/mo free credit on sign-up; then pay per use.

**Trade-offs.** Best accuracy + live traffic + Navigation SDK for a driver app, but
**per-call cost scales with fleet size**, closed ToS (can't resell raw Google routing
inside a competing product without the relevant license), no self-host.

### 2.5 MapmyIndia / Mappls — India-native commercial
Primary sources:
- [Mappls API catalog](https://about.mappls.com/api/)
- [Mappls Routes & Navigation](https://about.mappls.com/api/routes-and-navigation/)
- [Mappls Maps / SDKs](https://about.mappls.com/api/maps/)
- [Mappls Search & Geocoding](https://about.mappls.com/api/search-and-geocoding/)
- [Mappls Optimisation APIs & SDKs](https://about.mappls.com/api/optimisation/)
- [Mappls Pin (doorstep address)](https://about.mappls.com/mappls-pin/)

- India-native mapping company (Ola/Uber and many Indian logistics use it). **Map data
  tuned for India**: pin code, landmarks, non-latin names, last-mile addresses — a real
  advantage over OSM for Indian delivery stops.
- **Routes** (routing, ETA, turn-by-turn), **Matrix** (distance/time), **Geocoding &
  Search**, **Traffic**, **Navigation SDK**, **Optimisation APIs & SDKs** (route planning /
  VRP), and **Mappls Pin** (doorstep digital address resolution).
- Commercial: freemium sandbox + paid plans / **per-call pricing** by tier.
- Global coverage (238 nations) if the fleet expands.

**Trade-off.** Most India-accurate, directly solves last-mile address pain; but
**commercial from the start** (no zero-cost self-host) and per-call cost accrues with
volume. Best as the **scale-out / production paid layer**.

---

## 3. Decision matrix (pros / cons, column-style)

| Criterion | OSRM (self-host) | GraphHopper (self-host) | Valhalla (self-host) | Google Routes (commercial) | MapmyIndia/Mappls (commercial) |
|---|---|---|---|---|---|
| **Self-hosted (zero licence)** | ✅ Yes — free software | ✅ Yes (FOSS); hosted is paid | ✅ Yes — MIT | ❌ No | ❌ No |
| **India road/address quality** | ⚠️ OSM-driven; good trunk, weak last-mile/names | ⚠️ OSM-driven, similar | ⚠️ OSM-driven, similar | ✅ Excellent overall | ✅✅ **Best for India** (pin, landmarks, last-mile) |
| **Live traffic** | ❌ No (static profiles) | ⚠️ Mostly no / optional | ⚠️ Optional historical+live | ✅✅ Yes | ✅ Yes |
| **Matrix scale** | ✅ Very large N per call (self-host, no per-element cap) | ✅ Large (self-host) | ✅ Large (self-host), richer output | ⚠️ 625 elements/request, chunked; per-element cost† | ⚠️ Per-call limits; commercial |
| **VRP / optimisation** | ⚠️ `/trip` = single-route TSP only; you build VRP; no time windows/capacity | ✅✅ `/optimize` = multi-vehicle **time windows + capacity + breaks** | ⚠️ `/optimized` = TSP; flexible costing (auto/truck) but no built-in time-window VRP | ⚠️ Route Optimisation offering (partner/AI path), not raw VRP endpoint | ✅ Optimisation APIs & SDKs |
| **Turn-by-turn** | ✅ route + steps/maneuvers (sparse names) | ✅ rich instructions | ✅✅ rich narrative, multilingual incl. Indian locales | ✅✅ excellent + Navigation SDK | ✅✅ full Navigation SDK, India-tuned |
| **Cost** | 💸 Server + RAM/disk (see §4); $0 licence | 💸 Server + RAM; $0 licence (FOSS) | 💸 Server + RAM; $0 licence | 💵 Per-request/element† (free ~$90/mo credit) | 💵 Freemium → plans / per-call |
| **Ops burden** | Medium (build, host, update OSM extract) | Medium | Medium | None (API) | None (API) |
| **Owning vendor / ToS risk** | None (FOSS) | Low (FOSS) | Low (FOSS, MIT) | Moderate (ToS; can't resell raw routing) | Low-Moderate (commercial ToS) |

---

## 4. Hosting cost for an India self-host (OSRM / GraphHopper / Valhalla)

- **Data source:** India OSM extract (e.g. Geofabrik
  `https://download.geofabrik.de/asia/india-latest.osm.pbf`, ~2 GB PBF).
- **OSRM build chain:** `osrm-extract` → `osrm-partition` → `osrm-customize` →
  `osrm-routed`. India `.osrm` data (contracted graph + CH) is roughly **4–10 GB** for the
  car profile; build/partition transiently needs several GB more RAM.
- **Runtime RAM:** ideally **2–4 GB free RAM** for `osrm-routed` on India-sized data to
  avoid swapping; a 4 GB VPS is comfortable. Disk: ~15–25 GB (PBF + build intermediates +
  final `.osrm`).
- **Zero-cost hosting:** any Always-Free tier — e.g. Oracle Cloud Always Free ARM
  (4 vCPU / 24 GB RAM free) or a 2–4 GB free-tier instance — keeps the **₹0 monthly
  software overhead** promised by `AVANDAB_ZERO_COST_ARCHITECTURE.md` while replacing the
  public demo with a private instance.
- **Update cadence:** re-run the build on a schedule (e.g. monthly OSM extract) to keep
  roads fresh.

> Real cost is **engineering + ops time**, not money. The "zero-cost" promise is kept for
> the licence, but you take ongoing ownership of the OSM data pipeline (updates, drift,
> outages) as your responsibility.

---

## 5. Tolls / FASTag — is it part of routing APIs?

**Short answer: no. Routing APIs at best expose toll *cost / distance en-route*, but
toll/FASTag *payment and reconciliation* is a separate integration and is not part of any
routing API.**

- **What routing APIs actually give you on tolls:**
  - **Google Routes** exposes **toll-disclosure** fields (toll passes, toll roads, and
    toll *cost estimate* segments) when the appropriate field mask is requested — that is
    travel-time/distance toll info on the route, not payment.
  - **OSRM / Valhalla** can **exclude toll roads** at profile/routing level (avoid-tolls
    behaviour), but return no toll amount.
  - None of the compared APIs perform FASTag balance checks or toll payments.
- **FASTag reality:** FASTag (NHAI RFID for Indian toll plazas) is settled via the vehicle's
  FASTag account / bank. Integrating FASTag = separate SDK/partner integration (e.g. via a
  FASTag-issuer API or a toll/banking partner) — **plan it as its own module, not as a
  routing feature**.

> Consequence: your "route cost" model should treat **toll as a post-processing layer** on
> the routed distance/geometry (toll booth lookup + FASTag fees), independent of the
> routing engine chosen.

---

## 6. RECOMMENDATION — zero-cost first, paid later (phased)

### Recommendation summary
Keep the existing **OSRM Table API** as the **zero-cost distance-matrix** backbone (it
already powers your greedy VRP loop and is battle-tested for matrices), but stop relying on
the public demo for production: **self-host OSRM on a free-tier VPS with the India OSM
extract**. Add a **real VRP solver (time windows + capacity)** only when multi-vehicle
constraints actually matter — that is GraphHopper `/optimize` (self-hostable, free) rather
than a commercial API. When the business needs **live traffic**, **India-tuned last-mile
address accuracy**, or a polished **turn-by-turn driver app**, switch the *presentation /
dispatching* layer to a commercial India-native provider (MapmyIndia/Mappls) — keep OSRM
as the cheap matrix engine underneath where volume demands it.

### Phase 0 — now (zero cost, zero licence)
- **Keep OSRM** + your existing Table API client + greedy nearest-neighbour assignment.
- **Self-host OSRM on an Always-Free VPS** (Oracle Cloud Always Free ARM or a 2–4 GB
  free tier) with the **India OSM extract**; run a monthly rebuild.
- Add `/route` with `steps=true` for **turn-by-turn** and `/trip` for **single-route
  optimisation** — all ₹0.
- Route cost model: add **toll as a post-processing layer** (§5), not in the router.
- **Why:** zero monthly software cost; keeps the repo's zero-cost promise; private instance
  removes the public-demo production risk.

### Phase 1 — fleet grows to multi-vehicle / scheduled deliveries (still self-hostable)
- Add **GraphHopper `/optimize`** (self-hosted) when you need **multiple vehicles, time
  windows, capacity, breaks**. Build the depot→stops matrix from OSRM (or GraphHopper
  `/matrix`) and let `/optimize` do constrained routing.
- Or adopt **Valhalla** instead of OSRM **if truck costing, isochrones, or predictive
  traffic matter**; its flexible costing fits freight and its regional extracts are easier
  to scale.
- Still ₹0 licence (FOSS); you pay only in ops time.

### Phase 2 — production driver experience & live ETA (paid, India-native)
- Add **MapmyIndia / Mappls** (recommended for India) or **Google Routes** for:
  - **Live-traffic ETAs** (neither OSRM nor GraphHopper gives you this for free),
  - **India-tuned geocoding** (pin code, landmarks, last-mile) — Mappls is the strongest
    here,
  - a **turn-by-turn Navigation SDK** in the driver app (Mappls or Google Navigation SDK).
- Use the commercial **matrix** only where its accuracy/traffic pays for itself; push
  **high-volume N×N matrices back to self-hosted OSRM** to control per-element cost.
- **Cost control:** Google's 625-element/chunk matrix means large VRP matrices cost money;
  Mappls per-call plans scale with volume. Keep OSRM as the bulk matrix engine, commercial
  only for driver-facing / live-traffic paths.

### When to pick each (quick decision rule)
- **Zero budget + pure distance matrix / internal dispatch** → self-hosted **OSRM**.
- **Multi-vehicle, time windows, capacity, self-host, zero cost** → **GraphHopper
  `/optimize`**.
- **Truck costing / isochrones / predictive traffic, self-host** → **Valhalla**.
- **Live traffic + excellent turn-by-turn driver app, budget available** → **Google Routes**
  (global best accuracy) or **Mappls/MapmyIndia** (best India last-mile + local support +
  competitive pricing).
- **Best India address/last-mile + India-native + willing to pay per call** →
  **Mappls/MapmyIndia**.

### Bottom line
> **Run OSRM self-hosted (India extract) now for zero-cost routing + matrix + VRP baseline.
> Add GraphHopper `/optimize` (or Valhalla) self-hosted when VRP constraints or truck
> costing are needed — still ₹0. Add MapmyIndia/Mappls (or Google) commercial APIs in Phase
> 2 for live traffic, India-tuned geocoding, and a production turn-by-turn driver SDK —
> keeping OSRM as the high-volume matrix engine underneath. Tolls/FASTag are a separate,
> post-processing integration, not part of any routing API.**

---

## 7. Source URLs

**OSRM**
- OSRM HTTP API v5.x docs: https://project-osrm.org/docs/v5.24.0/api/
- OSRM backend (GitHub): https://github.com/Project-OSRM/osrm-backend

**GraphHopper**
- GraphHopper Directions API (OpenAPI): https://docs.graphhopper.com/openapi

**Valhalla**
- Valhalla documentation: https://valhalla.github.io/valhalla/
- Valhalla repo (MIT): https://github.com/valhalla/valhalla/

**Google Maps Platform**
- Pricing & billing (Global): https://developers.google.com/maps/billing-and-pricing/pricing?hl=en
- Pricing (India): https://developers.google.com/maps/billing-and-pricing/pricing-india?hl=en
- Routes API coverage: https://developers.google.com/maps/documentation/routes/coverage
- Compute Routes / Compute Route Matrix: https://mapsplatform.google.com/maps-products/routes/

**MapmyIndia / Mappls**
- API catalog: https://about.mappls.com/api/
- Routes & Navigation: https://about.mappls.com/api/routes-and-navigation/
- Maps / SDKs: https://about.mappls.com/api/maps/
- Search & Geocoding: https://about.mappls.com/api/search-and-geocoding/
- Optimisation APIs & SDKs: https://about.mappls.com/api/optimisation/
- Mappls Pin: https://about.mappls.com/mappls-pin/
