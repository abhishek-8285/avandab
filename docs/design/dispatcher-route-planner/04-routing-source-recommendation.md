# 04 — Routing Source Recommendation (India)

> Decision: which routing / turn-by-turn / distance-matrix source powers the
> **Best Route Planner** for an India-first fleet. Synthesized from the full,
> primary-source-cited research in
> `docs/12-ROUTING-DATA-SOURCE-RESEARCH.md` (read it for the complete
> comparison + all URLs) and grounded in what this codebase already uses.

---

## 1. Context this tells us

- The codebase already ships an **OSRM client** (`internal/route/optimizer/
  osrm.go`) calling the **OSRM Table API** for distance matrices, feeding a
  **greedy nearest-neighbour** VRP baseline, with `mock` / `osrm-public` /
  `osrm-selfhost` providers (`registry.go`).
- The repo's stated philosophy is **zero-cost architecture**
  (`AVANDAB_ZERO_COST_ARCHITECTURE.md`), and ROADMAP item **C5** already asks
  to reconcile the OSRM posture (the zero-cost doc says "routing via mock or
  public OSRM demo"). This recommendation resolves that item.

## 2. The decision in one line

> **Self-host OSRM (India extract) now for zero-cost routing + matrix + VRP
> baseline. Add GraphHopper `/optimize` (or Valhalla) self-hosted when real
> multi-vehicle constraints are needed — still ₹0. Add MapmyIndia/Mappls (best
> India fit) or Google in a later phase for live traffic, India-tuned
> geocoding and a production turn-by-by Navigation SDK — keeping OSRM as the
> high-volume matrix engine underneath.**

## 3. Phased plan mapped to this codebase

### Phase 0 — now (zero cost; matches existing architecture)
- **Replace `osrm-public` reliance for production**: point `osrm-selfhost`
  (`OSRM_URL` / `ROUTING_OSRM_URL` in `registry.go`) at a **self-hosted OSRM on
  a free-tier VPS** (e.g. Oracle Cloud Always-Free ARM — 4 vCPU/24 GB — or a
  2–4 GB free tier) with the **India OSM extract** (Geofabrik `india-latest
  .osm.pbf`, ~2 GB → ~4–10 GB `.osrm`, 2–4 GB runtime RAM; ~15–25 GB disk;
  monthly rebuild). ₹0 licence.
- **Keep the existing `Optimizer` interface + Table client** — the seam is
  right; this is only a backend-swap. Update `config.RoutingConfig` default to
  point at the private instance; keep `mock` as dev/test fallback.
- **Add `/route` (`steps=true`)** for turn-by-turn geometry and per-leg
  duration (feeds the board's map polylines + per-stop ETA — see doc 03 §4).
- **Toll = post-processing layer** (see §4), not in the router.

### Phase 1 — fleet grows to multi-vehicle / scheduled deliveries (still ₹0)
- When "many vehicles + delivery time windows + load capacity" is real, add
  **GraphHopper self-host `/optimize`** — the only self-hosted engine here that
  does actual multi-vehicle VRP with time windows + capacity + breaks (OSRM
  `/trip` is single-route TSP only, no constraints). Feed it the matrix from
  OSRM (or GraphHopper `/matrix`).
- Alternatively adopt **Valhalla** if **truck costing** (weight/height/hazmat),
  **isochrones**, or **predictive traffic** matter — flexible per-vehicle
  costing fits freight, and its regional extracts scale better.
- ✓ Matches the backfill: our solver in doc 03 §4 can either grow in-house
  (VRP on OSRM matrix) *or* delegate constrained VRP to GraphHopper behind the
  same `Optimizer` interface. **Provider stays behind the seam.**

### Phase 2 — production driver experience & live ETA (paid, India-native)
- Add **MapmyIndia/Mappls** (recommended India) or **Google Routes** when you
  need: **live-traffic ETAs** (no self-hosted engine offers this free),
  **India-tuned geocoding** (pin code, landmarks, last-mile — Mappls is the
  strongest), and a **turn-by-turn Navigation SDK** in the mobile driver app.
- **Cost control:** push **high-volume N×N matrices back to self-hosted OSRM**
  (Google caps Compute Route Matrix at 625 elements/request and bills per
  element; Mappls is per-call). Use commercial only for driver-facing /
  live-traffic paths.
- Reroutes on traffic/closure (Samsara Commercial Navigation behaviour) become
  possible only here.

## 4. Tolls / FASTag (the India moat from doc 01 §5)

**Routing APIs do NOT do FASTag.** Confirmed in the research: Google Routes
exposes **toll-disclosure** (toll cost/distance segments when requested, not
payment); OSRM/Valhalla can only **avoid toll roads**. FASTag payment and
reconciliation is a **separate integration** (FASTag issuer / bank API).

Consequence for the design: **toll is a post-processing layer** in
`planned_routes` / cost model (doc 03 §2 `toll_cost_est`), independent of the
routing engine. It consumes the existing `fastag_tags` / `fastag_transactions`
tables (already in the codebase) — a genuine differentiator no Western vendor
has, exactly as flagged in the product spec (P2.1).

## 5. How this feeds the design pack

| Doc / phase | Routing role |
| :-- | :-- |
| Doc 03 §4 solver | Greedy (existing) → real VRP (in-house on OSRM matrix, or GraphHopper `/optimize` behind the same `Optimizer` interface) |
| Doc 01 P1.1 | Constraint enforcement admits GraphHopper `/optimize` as a provider |
| Doc 01 P2.1 / doc 02 §5 | FASTag = post-processing layer over `fastag_*` (never a router feature) |
| Doc 01 P2.2 | Avoidance zones via geofence → routing bans (feeds OSRM exclude / Valhalla costing / Mappls) |
| Mobile driver app (doc 02 §5) | Turn-by-turn Navigation SDK = Phase 2 paid provider (Mappls or Google) |

## 6. Source URLs (full list in `docs/12-ROUTING-DATA-SOURCE-RESEARCH.md` §7)

- OSRM HTTP API v5.x — https://project-osrm.org/docs/v5.24.0/api/
- GraphHopper Directions API — https://docs.graphhopper.com/openapi
- Valhalla docs — https://valhalla.github.io/valhalla/
- Google Maps pricing (India) — https://developers.google.com/maps/billing-and-pricing/pricing-india
- Mappls API catalog — https://about.mappls.com/api/
- India OSM extract — https://download.geofabrik.de/asia/india-latest.osm.pbf
