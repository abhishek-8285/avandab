# Tech-Spec Index — Avandab/MVTMS Roadmap (Phases 0–6)

This folder holds **detailed, build-by-number tech-specs** for every incomplete
feature in the Avandab/MVTMS transport ERP. Each spec follows `_TEMPLATE.md`
(sections 0–12) and is grounded in verified `file:line` ground truth, so a
developer (or junior) can implement end-to-end: **API contract → DB migration →
UI → business logic → config → tests → future/GPS**.

> Scope decision (user): **Adapter interface + config-flagged mock** for every
> external integration (GSTN, EWB, FASTag, Accounting, telematics providers).
> No external credentials needed to build or test.

## How to use
1. Read `00-migration-ownership-index.md` first — it owns the migration number
   space so specs never collide. Repo head is migration `00039`; new specs
   reserve `00040` onward.
2. Read `_TEMPLATE.md` to understand the 13 sections each spec must answer.
3. Pick a feature, follow its §3 (DB), §2 (API), §5 (logic), §7 (tests).

## Spec map

| # | File | Area | New migrations | Status |
|---|------|------|----------------|--------|
| 00 | `00-migration-ownership-index.md` | Migration number ownership | — | foundation |
| 01 | `01-telematics-ingestion.md` | Telematics ingestion pipeline | 00040, 00041 | 0% built |
| 02 | `02-geofence-engine.md` | Geofence engine | 00042 | 0% built |
| 03 | `03-fuel-audit-scorecard.md` | Fuel audit + driver scorecard | 00043 | 0% built |
| 04 | `04-live-map-share-maintenance.md` | Live map + share + maintenance | 00044 | 0% built |
| 05 | `05-alerting-compliance.md` | Alerting + regulatory compliance | 00045, 00046 (+00059 telemetry_alerts rebuild) | 0% built |
| 07 | `07-gst-ewaybill-fastag.md` | GST e-invoice, EWB, FASTag | 00047, 00048, 00049 | stubs only |
| 08 | `08-settlement-accounting-docvault.md` | Driver settlement, accounting sync, doc vault | 00050, 00051, 00052 | dead/partial |
| 09 | `09-eventbus-tenancy-booking-trip.md` | Event bus, tenancy, booking, trip POD | 00053, 00054, 00055 | broken/missing |
| 10 | `10-auth-rbac-rag.md` | Auth hardening, RBAC, RAG | 00056, 00062 | critical gaps |
| 11 | `11-payment-razorpay.md` | Real Razorpay payment flow | 00057 | cosmetic/broken |
| 12 | `12-ui-completeness.md` | UI completeness, SSE, exports, PWA | (UI only) | missing |
| 13 | `13-mobile-app.md` | React Native driver app | (consumes API) | hardcoded demo |
| 14 | `14-graphql-grpc.md` | GraphQL + gRPC real impl | (reuse) | DEFERRED — removal recommended |
| 15 | `15-testing-ci.md` | Testing strategy + CI hardening | (tests) | no gates |
| 16 | `16-pnl-ops-experiments-founder.md` | PNL, ops, experiments, founder | 00058 | partial/broken |
| 17 | `17-gps-telematics-provider-strategy.md` | **GPS provider strategy** | (cross-cutting) | primary doc |
| 22 | `22-command-center-ux.md` | **Command Center one-screen UX**: ranked alert inbox, money strip, fleet context panel, bookings kanban, driver Paisa tab, kharcha OCR verification, compliance radar, WhatsApp channel | 00092–00096 | S0–S12 built; S13 flag-flip pending pilot |
| 23 | `23-scale-tiering.md` | **Scale tiering handoff**: telemetry retention, read replica, ingest sharding, Postgres cutover — trigger-gated, draft until R0 measurements | none reserved (00100+ on activation) | R0 built |
| — | `features-explainer.md` | Plain-English feature overview | — | reference |
| — | `_TEMPLATE.md` | Spec structure (sections 0–12) | — | template |

> `06` intentionally skipped to keep numbering aligned with the original
> roadmap (design specs 01–05 already used; 06 is the "core foundations"
> already present in code).

## Cross-cutting / sequencing guidance
- **GPS & telematics** (spec 17) is the strategic backbone: every GPS-consuming
  feature (01, 02, 03, 04, 05, 07 fuel, 08 payout, 09 trip start) runs behind a
  single `TelematicsProvider` interface. Implement 17 + 01 before 02/03/04/05.
- **Event bus** (spec 09) must be unified before 08/16 wiring (settlement,
  PNL, notifications) or you rebuild twice.
- **Multi-tenancy** (spec 09) and **auth/RBAC** (spec 10) are prerequisites for
  any multi-customer deployment — do them early.
- **Migrations**: always append; never edit existing `db/migrations/0000x`.
  `test/helpers.go` (`NewTestDB`) auto-applies migrations in tests — add a
  migration-exists assertion when you ship a new number.
- **Dual-write fast-path** (Spec 01) is REQUIRED before Spec 04 SSE;
  `company_config` @00042 (Spec 02) must precede specs that seed it (03/07/08/16).
- **Command Center UX** (Spec 22) layers on Phase 0–2 outputs (unified event
  bus, alert pipeline, settlements, telemetry SSE). Start only after Phase 0
  gates pass; ship stepwise (S1→S13) behind feature flags, one flag per step.

## Execution order (critical path — dependency-driven, not numerical)

### Phase 0 — Security & Core Foundations
Goal: stop the bleeding, fix security holes, unify core plumbing.
1. Spec 10 (Auth/RBAC/RAG) — patch the RAG arbitrary-file-read hole (unauth /api/rag/*, allow-list index dirs) + encrypted sessions + token revocation
2. Spec 09 (Event Bus & Tenancy) — unify the dual in-memory buses, kill 26+ hardcoded TenantID:"1" (enforce via CI grep gate), fix booking immutability + trip POD
3. Spec 11 (Razorpay) — server-side order + verify endpoint so revenue is recognized

### Phase 1 — Telemetry Backbone
Goal: real GPS flowing.
1. Spec 17 + 01 (Telematics) — TelematicsProvider interface, MQTT/REST doors, canonical pipeline (00040/00041) with DUAL-WRITE FAST-PATH (outbox durability + in-memory PositionEvent for SSE)
2. Spec 02 (Geofence) — dwell worker + ray-cast (00042) — UNBLOCKS company_config for all downstream specs
3. Spec 13 (Mobile) — fix auth parsing, remove mock data, connect real REST/MQTT

### Phase 2 — Operations & Financial Automation
Goal: GPS → business value.
1. Spec 04 (Live Map + Maintenance) — SSE hub (consumes dual-write fast-path), share links, maintenance blockers
2. Spec 03 (Fuel + Scorecard) — median-smoothing fuel-theft engine, scorecards (00043)
3. Spec 05 (Alerting + Compliance) — unified alert pipeline, storm batching, compliance gates (00045/00046/00059)
4. Spec 08 (Settlement + Doc Vault) — settlement persistence, TDS, doc vault (00050-00052)

### Phase 3 — Integrations & Scale
Goal: outside world + polish.
1. Spec 07 (GST/EWB/FASTag) — NIC/GSP mock adapters, E-Way lifecycle (00047-00049)
2. Spec 16 (PNL/Ops/Founder) — PNL snapshots, error dedup, founder digests (00058)
3. Spec 12 (UI) — missing fragments, CSV/PDF exports, PWA
4. Spec 15 (Testing/CI) — coverage floor, gosec/govulncheck, Playwright, anti-regression gates (tenant-hardcode grep + RAG-auth grep)

### Deferred / removed
- **Spec 14 (GraphQL/gRPC): DEFERRED — recommended REMOVAL.** REST + MQTT cover all current mobile/web needs; gRPC registers zero services; GraphQL is a mock. Revisit only if a typed consumer commits. No DB, no migration cost to defer.
