# Avandab Roadmap (2026-09-07)

> Survey of built state vs specs, phased forward plan. Sources: `docs/01–08`, `AVANDAB_ZERO_COST_ARCHITECTURE.md`, `tech-specs/00-migration-ownership-index.md`, `tech-specs/fleet-registry-sop-parity.md`, `AGENTS.md` critical path, `git log`, `FAILURE_ANALYSIS_2026-08-31.md`. `docs/*.md` supersede on conflict.

## Where we are (built)

**Stack** (`docs/01`): pure-Go binary, Chi router, SQLite WAL (`modernc.org/sqlite`), goose migrations, server-rendered HTML (Datastar/HTMX) + Leaflet OSM live map, in-memory event bus + transactional outbox, TCP `:5023` GPS ingest, MQTT `:1883`, Expo SDK 52 mobile app.

**Migrations:** `db/migrations/` = 122 files, head `00128_eway_bills_status_lifecycle.sql`; `db/migrations_pg/` mirrors at 122. Next free slot **`00129`**. `00068–00072` confirmed ABSENT (RESERVED-UNBUILT still true).

**Recent direction:** tenant fail-closed hardening + `DefaultTenant` fallback removal (`e74c040`); vehicle compliance hard-block restore (`e9c23ad`); founder-signal tenant scoping; PG dual-engine parity + CI gate; per-tenant company profiles (`00125`); self-registration → `org_admin`.

**Built per migration (spot-verified):** telemetry GT06/AIS-140 + provider parity (`00117`); trip state machine + dwell + detention; route/ETA jobs (`00066/00067`); GST engine + PDF/QR + settlement ledger + Razorpay; tenancy registry + trigger hardening (`00102–00105`); churn (`00073–00076`); files (`00077`); RAG RBAC (`00078`); leader leases (`00079`); driver lifecycle (`00108`); dispatch offers (`00109`); quotes (`00110`); settlement ledger (`00111`); multi-leg + multistop EWB (`00112/00113`); entitlements + subscription webhooks (`00114/00115`); push tokens (`00116`); comm outbox (`00118`); email pool (`00120`); booking/trip indexes + idempotency (`00121/00122`); work orders (`00123/00124`); fleet SOP parity (`00126` JSON API + SAP-tab HTML); EWB delivered lifecycle (`00127/00128`). Agent orchestrator + RL loop + approval gate. OpenAPI covers `/api/v1/vehicles` (+points/measurements).

**Zero-code-presence follow-ups (repo-wide grep):** `ZMOTM_MMS`/`ZMOTM_MR`, `IP41`/`IW28` (only in SOP spec doc); backhaul (one comment); STO portal / load board, ESG, fuel cards (index rows only). KMPL partial (template + fuel audit exist; SOP report set not built). Mobile: 17 screens but only `DispatchScreen`/`TripsScreen` are `.tsx` — rest are `.ts`, stub-vs-real unverified.

**Reality divergences (`AVANDAB_ZERO_COST_ARCHITECTURE.md` vs observed):** compute is TECNO-LE7 Android VPS + Cloudflare tunnel, not GCP e2-micro (§2 stale); mail converged further (`comm_outbox` + quota pool, §4B understates); DB is dual-engine PG parity, not single SQLite; OSRM-via-Docker contradicts "zero-Docker". `ALL_TECH_SPECS.txt` is a pointer stub, not a spec. Ownership index header stale (says head `00039`/range `1–117`; table runs to `00128`).

## Phase A — Stabilize (ship in AGENTS.md critical-path order)

- **A1. Secrets + mock-honesty audit** (`docs/08` §5). Rotate `COOKIE_SECRET`/`API_SECRET` off dev defaults; assert mock flags carpet `MOCK-` prefixes in non-prod. No migration.
- **A2. Tenant-hardening tail sweep** (`docs/06` §1). Re-run tenant lint; wire `scripts/tenant-lint.sh` into CI. No migration. *(2026-09-07: lint at 0 warnings.)*
- **A3. FK-health triage (61 pre-existing violations)** (SOP spec §10 notes). Read-only enumerate + disposition per table. No migration.
- **A4. Ops auto-checks green** (`FAILURE_ANALYSIS` auto-checks). Cron backup + ensure scripts, DNS forwarder, opencode `HOME=/` guard, `/tmp` trap. *(2026-09-07: crontab set, hooks on.)* No migration.
- **A5. Migration-index doc repair** (index `:1-5,110`). Fix header (head `00128`, range `1–128`), reconcile `00121+ reserved` vs allocated. Docs-only.
- **A6. OpenAPI↔router parity audit.** Diff every `/api/v1/*` mount against `paths:`. Docs/tests-only.
- **A7. PG-parity tail proof.** Confirm CI gate green on head + `sqlite2pg` dry-run clean. No migration.
- **A8. Vehicle legacy-delegation ADR** (SOP spec §10 Deferred). One paragraph: keep split vs finish delegation. No behavior change.
- **A9. EWB delivered-lifecycle staging proof** (`00127/00128`). Drive one trip to `DELIVERED`, assert events. No migration.
- **A10. Mobile stub inventory** (`docs/04`). Classify each screen stub vs wired with API binding per row. No migration.

## Phase B — Feature gaps (SOP non-goals; each ≤1 migration; next free `00129+`)

- **B1. Trip Start/Close `ZMOTM_MMS` (pp.6-8). `00129`.** Close-reading/date/time on Trip Close + Breakdown→notification hook; feeds Gate Register.
- **B2. Fuel issue entry (p.9). `00130`.** Station OP/CL readings → `PUMP` measuring points; expiry → `valid_to`.
- **B3. Maintenance plans `IP41` (pp.10-15). `00131`.** Scheduling from `annual_estimate` (`00126`) onto `work_orders` (`00123`).
- **B4. Maintenance notifications `IW28` + dispatch block (p.13). `00132`** (or no-DB if `work_orders` suffices). In-Process ⇒ `CanAssign` block.
- **B5. Reports `ZMOTM_MR` Gate/Fuel/KMPL/Breakdown (pp.16-20). `00133`.** Exact p.18 Vehicle-Master columns = acceptance test.
- **B6. Facility master / ZFID sync (p.1-2). `00134`.** Table per p.2 screenshot cols; `vehicles.facility_id` TEXT → FK (NULL still allowed for contractual).
- **B7. `00068` Backhaul matching (no DB).** Return-load suggestions on completed-trip corridors.
- **B8. `00069` STO portal + load board listings.** Shipper order portal + carrier search, tenant-scoped. *(Needs one-page spec first — no spec doc on disk.)*
- **B9. `00070` CX tracking timeline (no DB, uses `00044` share).** Public milestone timeline on share links.
- **B10. `00071` Fuel cards + accounting-sync extension.** Card ledger posts to sync log; kharcha cross-check (`00094`).
- **B11. `00072` ESG snapshots.** Per-period CO₂/km snapshot + report view.
- **B12. OpenAPI gap closure.** Work orders, dispatch offers, quotes, settlement ledger/payouts, entitlements/webhooks, push tokens, comm outbox, churn portal — path + schema + auth + e2e each. *(B7–B11 specs 19/20 have index rows but no spec docs — write one page each first.)*

## Phase C — Future bets

- **C1. Multi-instance safety.** Built: `worker_leases` (`00079`) + leader election. Remaining: ingest/API topology split, lease tuning, dwell/outbox duplicate fencing.
- **C2. PG cutover.** Built: mirror, `internal/datamigrate/`, `cmd/sqlite2pg/`, rebind, CI gate. Remaining: freeze→migrate→verify→flip→rollback runbook, Timescale decision (>5k-truck trigger), history archival.
- **C3. Commercialization.** Built: catalog/subscriptions/meters (`00114`), sub webhooks (`00115`), profiles (`00125`), flags (`00089`). Remaining: live pricing, prod webhook creds off mock, quota enforcement on, dunning/cancel flow. Mutating agent tools stay approval-gated.
- **C4. Live provider integrations.** EWB, GSTN, FASTag, accounting all mock-by-default: sandbox contract → prod creds → mock-honesty in non-prod → runbook each.
- **C5. Zero-cost doc reconciliation.** Rewrite or supersede `AVANDAB_ZERO_COST_ARCHITECTURE.md` §§2/4/6 (device reality, mail path, PG path, OSRM posture).
- **C6. Network-effects bets (post-B7–B11).** Load-board liquidity, STO self-serve, CX timeline as wedge, ESG tenders, fuel-card float.

## Risks & open questions

1. Single-node fragility (2026-08-31 load-28/502 on 5.6 GiB device) vs 5k-truck claims — C1/C2 hedge; no scale promises before PG-cutover proof.
2. 61 pre-existing FK violations — A3 may promote some to bug fixes with `00129+` migrations.
3. Mock-by-default providers safe only while flags stay true; guard staging mocks from prod compliance.
4. Index drift invites number collisions (past `00081/00084/00085`) — do A5 before any B-ticket.
5. B7–B11 blocked on one-page specs each (owner, state machine, acceptance).
6. B6 soft-depends B1 Gate Register — confirm order B6∥B1.
7. OpenAPI survey was partial (through `/telemetry/*`) — A6 may enlarge B12.
8. Mobile `.ts` screens may be stubs — A10 decides mobile tickets per feature.
9. Agent mutating tools stay gated — auto-billing must never bypass `/agent-actions`.
