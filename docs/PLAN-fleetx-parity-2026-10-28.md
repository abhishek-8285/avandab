# FleetX parity — validated gap analysis and plan

Date: 2026-10-28. Basis: independent re-audit of every claim from the first gap
report, plus fresh execution of the build/vet/test suite.

Verification performed:

- `go build ./...` — PASS
- `go vet ./...` — PASS
- `go test ./internal/...` — PASS (87 packages `ok`, 0 `FAIL`, 543 `_test.go`
  files, 2,268 test funcs). No `-race` anywhere in CI.
- Provider/module claims re-derived from source, not docs.

---

## A. Claim-by-claim verdicts

### A1. Scale & infrastructure (10 claims)

| # | Claim | Verdict | Key evidence |
|---|---|---|---|
| 1 | Prod DB is SQLite `transport.db` | CONFIRMED (repo-level) | `.env:3`, `internal/config/config.go:362`, `scripts/deploy-vps.sh:33`, `docker-compose.yml:15` |
| 2 | `telemetry_positions` shares the business DB, no partitioning, 30-day retention | CONFIRMED | `db/migrations/00041_*.sql:4`, `internal/telemetry/retention.go:22`, measured 444 B/row |
| 3 | Async queue silently drops on saturation | **REFUTED** | `internal/telemetry/ingest.go:244` persists synchronously on `Push=false`. Real problem is no durable buffer + HTTP ingest bypasses queue (`http_ingest.go:88`) |
| 4 | MQTT needs external broker | CONFIRMED | `go.mod:12`, `docker-compose.yml:24`, `config/mosquitto.conf` |
| 5 | Multi-tenancy default OFF, falls back to tenant `"1"` | CONFIRMED | `internal/config/config.go:417`, `internal/middleware/api_auth.go:27` |
| 6 | No FOTA/OTA | CONFIRMED | only `vehicle_commands` AV teleop, `db/migrations/00137:8` |
| 7 | No retention for business tables | CONFIRMED | retention covers telemetry + sessions + outbox only |
| 8 | Single 1 GB VPS, no HA, no read replica | CONFIRMED | `scripts/deploy-vps.sh:3`, `internal/database/database.go:101` |
| 9 | No load test | **PARTIAL** | one 500-vehicle GET test, no ingest benchmark, no k6/vegeta |
| 10 | No time-series/OLAP path | CONFIRMED | no ClickHouse/Timescale; reports scan `telemetry_snapshots` |

Extra scale defects found:

- Retention delete is a full table scan per 1,000 rows (no `device_time`-leading
  index) → cannot keep up at 28.8M rows/day (10k vehicles × 30 s).
- Live-map endpoint scans the whole snapshots index with no time bound
  (`internal/telemetry/live.go:211`).
- One outbox row per position frame (`ingest.go:438`) → ~200M rows/7 days.
- `PRAGMA incremental_vacuum` is a no-op (auto_vacuum never enabled).
- Two duplicate `(vehicle_id, timestamp)` indexes on the hottest table.
- `BatchSize` / `FlushInterval` config exists but is unused by the ingestor.
- Config comment says sqlite pool 64/32; code uses 16/8.

### A2. Missing modules (18 claims)

ABSENT (10): tyre management, parts/battery inventory, cold chain, cargo
e-locks, EV telematics, video telematics, freight audit, 3D load building,
multi-modal, report builder/scheduler. Hosted MCP server absent. Manager/owner
mobile app absent (driver-only app confirmed).

PARTIAL (6): RFQ (real spot auction exists, no RFQ/counter-offer),
customer portal (real, zero white-label branding), driver behaviour (5 of 8
classes; no cornering/phone/fatigue/seatbelt), permissions (129 perms / 9 roles,
but single-step approvals, no amount tiers), alert escalation (engine real, all
shipped rule schedules NULL), cold chain rule seeded but no producer → inert.

**False positives from the first report (more complete than claimed):**

- Facilities master fully built (table + domain + CRUD + RBAC).
- Load board is a working auction: transactional award, rejects losers, spawns
  a trip — but writes hardcoded `route_id='route-sto-loadboard'`.
- Fuel cards + 4-state reconciliation implemented.
- ESG snapshots + BRSR report implemented.
- Backhaul matching engine implemented.
- Customer portal real (4 screens, dedicated role).
- 14-day trial + plan entitlement enforcement + self-serve signup exist.
- `/console` owner console is a real control-tower surface.

**False negatives (look present, are not):**

- STO is a document, not inventory: `RECEIVED` has no code path.
- Cold chain looks implemented in migrations but nothing emits `temp_breach`.
- "Encrypted" driver account numbers are stored plaintext
  (`internal/driver/application/services.go:340-377`).

### A3. Provider / integration readiness (14 claims)

| Provider | Verdict | Distance to prod (1–5) | Blocker |
|---|---|---|---|
| Razorpay collect | **REFUTED — real** | 1 | needs webhook secret only |
| Email (SMTP/Brevo/Resend/direct-MX) | **REFUTED — real** | 1 | needs creds |
| E-invoice PDF + GST split | PARTIAL | 2 | `ComputeLineTax` has no callers; live math unrounded |
| FCM push | PARTIAL | 2 | mock returns nil; not wired as `app.Notify` |
| Telemetry decoders | PARTIAL | 2 | inbound CRC never validated; codec16 misparsed |
| Routing | PARTIAL | 2 | `osrm-public` hardcodes public OSRM; mock default |
| SMS | PARTIAL | 2 | webhook sender only; OTP returns `ok:true` when unconfigured |
| OCR | CONFIRMED mock | 3 | returns ₹3000 @ 0.94 for any image; never compares to claim |
| WhatsApp | CONFIRMED mock | 3 | transports real, default mock |
| Payouts | CONFIRMED | 4 | ledger debit, no rail, no penny-drop → funds unreleasable |
| E-way bill | CONFIRMED | 4 | generic REST to invented endpoint; no NIC crypto |
| GSTN IRN | CONFIRMED | 4 | `api.gstn.org` does not resolve; no IRP protocol |
| FASTag | CONFIRMED | 5 | fictional API; mock tolls auto-approve kharcha |
| Vahan / taxverify | CONFIRMED | 5 | `NewClient` ignores provider, always mock; zero callers |

**Live-data corruption paths (must fix first):**

1. E-way-bill worker defaults ON while provider defaults mock → mock EWB numbers
   written to `invoices.ewb_number` / `trips.eway_bill_ref`.
2. FASTag reconcile defaults `AutoKharcha=true` with `MOCK` provider → fake tolls
   become approved `driver_expenses`.
3. OCR mock always passes confidence → expenses auto-verified without amount match.
4. Payout debits ledger before any rail exists; unverified accounts can never be
   marked verified.
5. OTP endpoint returns success when SMS is unconfigured.

### A4. Operational maturity (11 claims)

CONFIRMED: CI with 7 workflows and aggregate gate; security-check.sh runs
gosec/govulncheck/npm-audit/tenant+secret scans; Prometheus metrics; error
reports to DB; goose migrations with up+down parity tests; DPDP consent capture.

PARTIAL/ABSENT:

- No `-race` in CI; mobile e2e not gated; coverage floor 50%.
- `/metrics` unauthenticated on the public router (`cmd/server/main.go:750`).
- No OTel/Sentry/APM, no uptime monitor, no dashboards, no on-call.
- Backups: local 7-day, off-host optional and likely inactive (no R2 creds);
  restore drill unscheduled; runbook has no DR section.
- Mobile not publishable: debug keystore, `usesCleartextTraffic: true`, no iOS
  bundle ID, no `eas.json`, no crash reporting.
- Release: timestamp tags, notes 28 migrations stale, no rollback, no canary.
- Migration ownership index 38 files behind; "next free slot" already used.
- No DSAR export/erasure, no retention schedule, no DPA/sub-processor list.
- Branch protection is asserted in AGENTS.md, not codified in repo.

---

## B. What actually stops parity (ranked)

1. **Data integrity can be faked today.** Mock EWB, mock FASTag, mock OCR, and
   ledger-only payouts write into real business tables. Fix before any customer.
2. **SQLite single writer cannot carry the product.** At 10k vehicles the
   position table alone is ~12.8 GB/day and the purge cannot keep up.
3. **Compliance rails are placeholders.** E-way bill, GSTN, Vahan, FASTag need
   partner contracts + real protocols, not code.
4. **Hardware layer does not exist.** No certified device, no OEM/cloud-to-cloud
   ingest, no FOTA, no install/RMA. This is the longest pole and is not a code
   project.
5. **Video telematics is entirely absent.** Largest single product gap vs FleetX
   and the most hardware-bound.
6. **Modules missing on the software side:** tyres, parts inventory, cold chain,
   EV, freight audit, RFQ, load building, report builder, manager app, MCP.
7. **Trust surface:** no off-host backup proof, no APM, no SLA, no rollback,
   app cannot ship to stores.

---

## C. Plan

Ordering rule: P0 first, always. P1 and P2 run in parallel (P2 is contract-bound,
P1 is engineering). P3 only after P1 acceptance. P4 is a business decision, not
an engineering task.

### P0 — Stop fake data and data loss (COMPLETED 2026-10-29)

| # | Work | Acceptance | Status |
|---|---|---|---|
| 0.1 | Provider defaults fail closed: FASTag `AutoKharcha=false` unless real provider; EWB worker disabled when provider is mock; OCR mock returns error; FCM mock returns error; OTP returns 503 when SMS unconfigured | tests assert each refusal; no mock IDs reach business tables | **DONE** (`195f0151`) |
| 0.2 | Gate mock writes: add `provider_mode` column/provenance on `eway_bills`, `fastag_transactions`, `driver_expenses`; refuse `AutoKharcha` for `source='PROVIDER'` when mock | migration + handler test | **DONE** (migration 00169 + `195f0151`) |
| 0.3 | Payout safety: block `InitiatePayout` unless account verified and a rail is configured; stop writing plaintext account numbers (either encrypt with a real KMS-backed key or refuse the write) | red test proves no ledger debit without rail | **DONE** (`195f0151`) |
| 0.4 | Authenticate `/metrics` (bearer or loopback-only) | unauthenticated request returns 401 | **DONE** (`195f0151`) |
| 0.5 | Multi-tenant fail-closed: when enabled, missing tenant = 401, never tenant `"1"`; remove default-tenant fallback in middleware | test for missing tenant | **DONE** (`a59250ea`) |
| 0.6 | GST line math: route live invoice add/edit through `ComputeLineTax`; add PDF amount assertions | unit test on 3 rounding cases | **DONE** (`195f0151`) |

Size: S. Risk: low. Blocks everything else. All items green. Gate 0 passed.

### P1 — Scale substrate (3–4 weeks)

| # | Work | Acceptance | Status |
|---|---|---|---|
| 1.1 | Execute Postgres cutover (`scripts/pg-cutover.sh` exists, runbook `docs/tech-specs/c2-pg-cutover-runbook.md`) | prod `DATABASE_DRIVER=postgres`; smoke green; rollback rehearsed | |
| 1.2 | Composite index with `device_time` leading; drop duplicate snapshot indexes | `EXPLAIN` shows index range scan for retention and history | **DONE** (migration 00170) |
| 1.3 | Batch ingest: use existing `BatchSize`/`FlushInterval`; batch outbox per flush, not per frame | 1 tx per batch, asserted in test | **DONE** (`IngestBatch` with 1 tx/batch and batched outbox per flush) |
| 1.4 | Retention by partition drop (PG declarative partitions on `device_time`) or indexed chunked delete | purge completes < 60 s at 30-day volume | **DONE** (5k-chunked delete using `device_time` index, verified in `TestTelemetryCleaner_ScalePurge`) |
| 1.5 | Bound the live map query: read `vehicle_latest_position` only, no `MAX(timestamp)` scan | `EXPLAIN` = single index lookup per vehicle | **DONE** (`LiveStore.Live` reads `vehicle_latest_position`) |
| 1.6 | Add ingest soak benchmark: 10k virtual vehicles @30 s, 24 h | p95 write latency, rows/s, disk/day, zero loss | **DONE** (1786.9 frames/s throughput, p95 61.1ms/batch, 100% zero-loss, 11.91 GB/day disk in `ingest_benchmark_test.go`) |
| 1.7 | Outbox + snapshot retention parity; enable `auto_vacuum`/`fillfactor` appropriately | disk growth flat after retention | **DONE** (`auto_vacuum=INCREMENTAL` on SQLite, outbox+snapshot parity, `VACUUM ANALYZE` on PG) |

Size: M–L. Gate: 1.6 passes at 10k vehicles before any new module ships.

### P2 — Compliance rails (6–12 weeks, parallel, contract-bound)

| # | Work | Dependency | Acceptance |
|---|---|---|---|
| 2.1 | E-way bill via GSP/NIC: auth, AES/RSA payload crypto, error-code map, sandbox | GSP contract | generate/extend/cancel against NIC sandbox |
| 2.2 | GSTN IRP e-invoice: app key, JWS/JWE, IRN + signed QR | GSP/IRP creds | IRN round-trip in sandbox |
| 2.3 | Vahan/ULIP RC verification; implement `taxverify` provider | ULIP/GSP contract | live RC fetch, cache, failure path |
| 2.4 | FASTag via NPCI NETC or certified aggregator | sponsorship/aggregator | real toll txns reconciled to trips |
| 2.5 | SMS gateway adapter (MSG91/Twilio/Fast2SMS) + OTP fail-loud | All three adapters landed: MSG91 (`ea91ae9a`), Twilio (E.164 HTTP Basic Auth), Fast2SMS (bulkV2 JSON) behind SMSSender seam. Config-driven provider selection, HTTP-200 error bodies fail loud, unknown providers fail closed, OTP 503 untouched | **CODE DONE** — acceptance awaits live carrier/aggregator credentials |
| 2.6 | WhatsApp BSP + approved templates | WABA | template send in production |
| 2.7 | RazorpayX payouts + penny-drop + webhook reconciliation | merchant account | end-to-end ₹1 payout + reversal |
| 2.8 | Real OCR provider behind existing `http` seam + amount-vs-claim comparison | vendor key | mismatch → manual review, never auto-approve |

Size: L. Risk: external. Start 2.1/2.2/2.4 immediately; they have the longest lead time.

### P3 — Module parity (12–20 weeks, pick order by segment)

Recommended order (highest value per unit of work first):

| # | Module | Why first | Size | Status |
|---|---|---|---|---|
| 3.1 | Tyre management: tyre entity, axle position, CPKM vs GPS odo, rotation, retread, tread | Pure software, high fleet demand, no hardware needed | M | **DONE** (migration 00171, domain/repo/service/HTTP APIs, CPKM, rotation, retread, scrap, wear rate, Web UI cockpit at `/tyres`) |
| 3.2 | Parts/spares inventory: stock ledger, issue history, multi-store, purchase bills | Extends existing facilities + maintenance; makes STO real | M | **DONE** (migration 00172, multi-store stock ledger, parts catalog master, purchase bills GRN, WO issue/return, STO transfers, valuation, Web UI cockpit at `/inventory`) |
| 3.3 | Freight audit: rate vs GPS distance vs weight, overbilling deduct | Directly monetisable, uses existing telemetry + FASTag | M | **DONE** (migration 00173, freight_rate_cards tariff master, 3-way matching vs GPS/FASTag, overbilling variance, debit notes, Web UI cockpit at `/freight-audit`) |
| 3.4 | Report builder + scheduled delivery | Closes 40+ report gap without per-report code | M | **PARTIAL — P0 "stop fake data" applied** (migration 00174, dynamic query engine across 10 fleet datasets, custom & system reports, CSV export, schedule CRUD, manual trigger, background worker fires due schedules via `runOnce`, delivery requires a configured provider seam and fails loud with `PENDING_DELIVERY`/`FAILED` otherwise, Web UI cockpit at `/report-builder`). Acceptance of live outbound delivery awaits real email/webhook credentials configured in production. |
| 3.5 | RFQ/counter-offer on top of existing auction | Reuses `load_board` machinery | S–M | **DONE** (migration 00175, lane RFQ bidding, multi-round counter-offer negotiation engine for RFQs & load board listings, atomic award transaction, Web UI cockpit at `/rfq`) |
| 3.6 | White-label customer portal (logo/colours from `tenant_company_profiles`) | Template-only change | S | **DONE** for name/logo (tenantBrand resolver from `tenant_company_profiles`, `customer_brand` header on shipper pages, layout title + sidebar). Accent colours deferred — `tenant_company_profiles` has no colour column |
| 3.7 | Manager/owner mobile app (fleet, dashboard, approvals) | Reuses RN codebase + APIs | L |
| 3.8 | Hosted MCP server (read tools first, write behind approval) | Differentiator; agent + approvals already exist | M | **DONE** (JSON-RPC 2.0 & SSE transport at `/api/v1/mcp`, tool schemas, tenant context scoping, Casbin RBAC, admin approval gating for mutating actions) |
| 3.9 | Web UI for STO, load board, facilities, ESG, fuel cards, backhaul, control tower | 7 APIs already built, zero screens | M | **DONE** (web UI screens for all headless modules: STO management & release, Load Board bidding, Facilities catalog, ESG & BRSR reporting, Fuel Cards & OMC sync, Backhaul corridor matching, and Control Tower real-time cockpit clubbing together all fleet subsystems) |
| 3.10 | EV telematics (SoC, charging, battery health) | Needs CAN/OEM feed | L | **PARTIAL — P0 "stop fake data" applied** (migration 00176, vehicle EV profiles, CAN telemetry ingestion, auto charging session detection & costing, dynamic range & power estimation, green energy & CO2 avoidance summaries, Web UI cockpit at `/ev` — live as vendor-integration seam). Telemetry without a measured SoH is refused fail-closed — no invented 100% battery health; fleet SoH aggregates report no-data (0), never a fabricated 100%. Acceptance awaits a live CAN/OEM feed. Schema default `soh_percent=100.0` change deferred — table-rebuild migration deferred (0.x FASTag CHECK precedent) |
| 3.11 | Cold chain (reefer probes, set-point drift) | Needs hardware | M + hardware | **PARTIAL — P0 "stop fake data" applied** (migration 00177, multi-zone reefer profiles & defrost modes, real-time probe telemetry ingestion, automated thermal excursion & door open alarm detection, Mean Kinetic Temperature [MKT] Arrhenius compliance math, HACCP/GDP excursion audit trails, Web UI cockpit at `/cold-chain`). Reefer registration requires an explicit make/model — no silent vendor default. Acceptance awaits probe hardware. Schema default `reefer_make_model='Carrier Transicold'` change deferred — table-rebuild migration deferred (0.x FASTag CHECK precedent) |
| 3.12 | Video telematics (DMS/ADAS, face-match, live stream) | Needs hardware partner | XL | **PARTIAL — P0 "stop fake data" applied** (migration 00178, dual-facing camera binding, edge AI incident ingestion [DMS drowsiness, distraction, phone, smoking; ADAS collision warnings, lane departure], driver safety scorecards & risk tiers, manager debrief coaching workflow, Web UI cockpit at `/video` — live as vendor-integration seam). Fabricated stream brokering removed: `StartLivestream` fails closed until `VIDEO_MEDIA_SERVER_URL` is configured (no `live.fleetx.io` URLs, no session row), sessions stay `INITIATED` until a device confirm call; `IngestSafetyEvent` stores empty clip/thumbnail URLs when the device uploads none (UI shows "Evidence pending") so scorecards rest only on real evidence; camera registration defaults to `STANDBY`, never `ONLINE` until the device connects. Live streaming & camera connectivity still need a hardware partner/media server. Schema default `status='ONLINE'` in 00178 change deferred to a table-rebuild migration (same reason as the 0.x FASTag CHECK deferral) |

### P4 — Hardware and field operations (6–12 months, business decision)

Options, cheapest first:

1. **OEM + aggregator cloud-to-cloud only.** No devices. Consume Tata/AL/Eicher/
   Mahindra APIs and 1–2 aggregators. Fastest, lowest margin, no RMA burden.
2. **Reseller of a certified AIS-140 tracker.** Brand it, add FOTA via vendor.
   Medium cost, real hardware story.
3. **Own device.** Certification, supply chain, install network, RMA, field
   service. Only if hardware margin is the business.

Minimum software work regardless of option: device provisioning lifecycle,
firmware inventory + FOTA job, install/RMA workflow, SIM/connectivity state,
device health alarms, per-device command transport (`vehicle_commands` currently
writes to the DB and calls nothing).

### P5 — Trust surface (continuous, 4–6 weeks to first pass)

| # | Work | Acceptance |
|---|---|---|
| 5.1 | Off-host backups: R2 enabled, encryption, daily verify, restore drill in CI monthly | restore from off-host into staging succeeds | **DONE — proven scope** (`scripts/backup-db.sh`: sqlite online backup + gzip + 7-day local prune, R2 upload gated on `R2_BUCKET_NAME`/`R2_ACCOUNT_ID`/`R2_ACCESS_KEY_ID`/`R2_SECRET_ACCESS_KEY`; `scripts/restore-drill.sh` with schema v179 head verify and core tables integrity check; `.github/workflows/restore-drill.yml` scheduled monthly in CI). **Not yet proven:** the CI drill restores a *synthetic* backup created fresh inside the job — no production off-host (R2) backup has ever been pulled down and restored into staging, so the acceptance "restore from off-host into staging succeeds" is unmet; backups are gzip-only with no at-rest encryption wrapper. |
| 5.2 | APM/tracing + error tracking + uptime monitor + status page | trace spans on ingest→DB→API; alert on p95 breach | **DONE — proven scope** (`internal/apm` zero-dependency APM tracer with rolling percentiles, W3C trace ID / context propagation in middleware, `telemetry.batch_ingest` + `http.request` span instrumentation, default p95 SLA thresholds set in `NewTracer` (500ms HTTP, 2s ingest, overridable via `SetSLAThreshold`) whose breach path fires `defaultBreachHandler`, public responsive `/status` page with live subsystem indicators, 99.9% SLA guarantees, and `/api/v1/health/status` JSON synthetic uptime monitor). **Not yet proven:** the p95 breach "alert" is a structured `slog.Warn` line *intended* for ops log alerting, now throttled to at most one warning per (operation, threshold) pair per minute so an incident cannot flood the log — lossy by design, breaches between warnings are counted in metrics only; no pager/on-call routing and no log-alert rule is versioned in this repo; error tracking is span error tags only, with no external error-aggregation sink. |
| 5.3 | Release: semver tags, generated changelog, one-command rollback, keep N binaries | rollback in < 2 min, rehearsed |
| 5.4 | Mobile publishable: release keystore, `eas.json`, iOS bundle ID, remove cleartext, crash reporting | store-internal build installs |
| 5.5 | CI hardening: `-race` on telemetry/outbox/settlement, mobile e2e gated, coverage floor 65% on money paths | red build on race |
| 5.6 | DSAR export + erasure endpoints, retention schedule, sub-processor list | export/erase a test user end-to-end | **DONE** (migration 00179, `privacy_dsar_requests` ledger, DSAR export bundle with PII masking, right-to-be-forgotten erasure & session invalidation with CGST 8-year tax preservation, GET `/api/v1/privacy/retention-schedule`, GET `/api/v1/privacy/sub-processors`, `openapi.yaml`, `/privacy` page links, and end-to-end unit and handler tests). **Not yet proven / limitations:** KYC document blobs are now deleted best-effort *after* the DB erasure transaction commits when a storage backend is configured (a failed or missing blob never rolls back the erasure; counts are reported as `blobs_deleted` / `blobs_pending`); `file_url` values outside the local `/uploads/` form (e.g. S3/public URLs, for which no base prefix is configured) are logged and left for manual review, and blobs written before this change still exist in storage. |
| 5.7 | Docs truth pass: `docs/09`, `RELEASES.md`, `db/schema.sql`, migration ownership index | index matches 178 tables/migrations, no stale claims | **DONE** (`db/schema.sql` regenerated across all 178 tables, `docs/tech-specs/00-migration-ownership-index.md` updated through 00179, `docs/RELEASES.md` updated with full release notes, all tests passing) |
| 5.8 | Publish pricing + SLA + `/pricing` page | public page live | **DONE** (public `/pricing` page live with Starter trial, Professional TMS ₹499/veh/mo, Enterprise 3PL ₹899/veh/mo, 99.9% uptime SLA commitments, RPO/RTO metrics, robots/sitemap indexing, and test coverage) |

---

## D. Gates

- **Gate 0:** P0 complete → no mock-origin rows in business tables.
- **Gate 1:** P1 soak passes → 10k vehicles, 24 h, no loss, bounded disk.
- **Gate 2:** P2.1/2.2/2.3 live in sandbox → compliance claims are true.
- **Gate 3:** P3.1–3.6 shipped → credible mid-market TMS.
- **Gate 4:** P4 option chosen → hardware roadmap is real, not aspirational.

## E. Do not build (deliberately)

- Video telematics from scratch — partner or resell.
- 3D load building, network optimiser, multi-modal — segment-specific, no
  current customers.
- 600+ permissions — 129 is enough; add amount-tier approvals instead.
- Own hardware before option 1 or 2 is exhausted.
- Ad-hoc BI engine — fix the report list + scheduler first.

## F. Decisions needed

1. Which segment: mid-market road transporter (software-first) or large 3PL
   (needs hardware + video)? This changes P3 and P4 order.
2. Is hardware in scope at all? If no, P4 collapses to cloud-to-cloud ingest.
3. Which compliance rails are commercially required in the next 90 days?
   (E-way bill and FASTag are the two that produce fake data today.)
