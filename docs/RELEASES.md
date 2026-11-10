# Release Notes

Append-only log of shipped releases. Each entry: scope, migrations,
behavior changes demanding operator attention, and proof.

---

## b83d328f — GPS hardening + onboarding completion + mobile dead-code purge (2026-09-08)

### Migrations (sqlite + pg mirror, all up/down-tested)
| # | Change | Data note |
|---|--------|-----------|
| 00131 | `drivers.license_number` / `license_expiry` nullable | `DL-PENDING` backfilled to NULL/NULL |
| 00132 | `vehicles.insurance_expiry` / `fitness_expiry` / `permit_expiry` nullable | NO backfill — fabricated +1y indistinguishable from real |
| 00133 | `users.email_verified_at` (NULL = unverified) | Badge only; nothing gates on it |
| 00134 | `telemetry_snapshots.ts_unix` epoch clock | Backfills strftime-parseable rows; Go-String rows stay NULL (honest, never silently wrong) |

sqlc regen: `TelemetrySnapshot.TsUnix`. Index head → `00134`, next free `00135`.

### Behavior changes (operator-visible)
- **Live map**: vehicles silent longer than 2× stale window drop off instead of lingering as permanent `no_signal` ghosts.
- **SSE stream** (`/api/v1/telemetry/stream`): now carries device-health alerts, SOS, and trip lifecycle events; **tenant isolation enforced** — previously any authenticated caller received every org's positions. Trip bus payloads without a tenant stamp (legacy) still pass; stamped events are strictly matched.
- **Sync batch**: mobile must send per-log `id` (it now does); server acks exact `synced_ids` and the client reconciles exactly — retries are dedup-safe.
- **AIS-140**: `*XX` checksum verified when present; corrupted frames rejected (missing checksum still tolerated).
- **Unknown mobile command types**: fail loud as FAILED with no network call (previously POSTed to a nonexistent endpoint and stalled the queue).
- **Onboarding**: unknown license/vehicle dates are NULL end-to-end (no fabricated `DL-PENDING` / +5y dates); email verification is badge-only; reviewer-only gates on license/vehicle verification and assignment.
- **Deleted (mobile, all zero-import proven)**: esign, documentVault, dataRights, fastag, ewaybill clients + EWayBillCard, FirstTimeSetupScreen (1077 lines, hardcoded `DL1LN9999` demo plate), phantom telemetry pipeline (sessions/events endpoints never existed) + second bg task + 3rd sqlite db.

### Rollout notes
- 00134 backfill is online-safe (plain ADD COLUMN + UPDATE, no rebuild).
- Existing mobile installs self-heal `offline_gps_logs` parity columns via upgrade ALTERs at startup.
- Auto trip transitions (`auto_reach_pickup`, `auto_start_transit`) remain default-OFF per tenant — deliberate rollout flags, not a bug.
- Deferred (roadmap, not gaps): ignition TripStart/TripStop (Phase 2), Timescale path, `ts_unix` adoption in readers, trip-event tenant for non-forwarded types.

### Proof
- `go build ./...`, `go vet ./...`, `gofmt` clean; full `./internal/...` + `./db/...` green; mobile `tsc` + 52/52 suites (336 tests).
- Security gate (`security-check.sh`) 8/8 green; pre-commit sqlc-integrity hook caught and fixed one regen miss before commit.
- Live HTTP A/B: pre-fix binary leaks a 2020 ghost as `no_signal`; fixed binary excludes it. Zero-ID sync probe: 3 fixes → 1 position pre-fix.

---

## 544247ca — Storage, scale & ops hardening cycle (2026-09-10)

### Migrations (sqlite + pg mirror; up/down recorded in ownership index, not re-verified this refresh)
| # | Change | Data note |
|---|--------|-----------|
| 00135 | `vehicles.registration_number` UNIQUE → UNIQUE(`tenant_id`, `registration_number`) + `vehicle_latest_position` UNIQUE(`tenant_id`,`vehicle_id`) | Plates become per-tenant (telemetry H3+L10) |
| 00136 | `telemetry_positions.vehicle_id` `''` → NULL backfill | idempotent; down is documented no-op |
| 00137 | `vehicle_commands` table + `av_operator` role | AV teleoperation deck |
| 00138/00139 | Partial + list scale indexes (outbox, alerts snooze, fuel audits, sessions, vehicles/drivers, invoices, payments, audit logs) | read-only perf |
| 00140 | `files.tenant_id` + triggers | **Operator-visible: file reads are now strictly tenant-scoped** — closes cross-tenant licence/RC/POD read via UUID |

### Behavior changes (operator-visible)
- **Files** (`00140`): viewer/`%:read` roles can no longer read another org's uploads by UUID. Existing rows backfilled to bootstrap tenant — verify multi-tenant file access after deploy.
- **Plates** (`00135`): two tenants may now hold the same registration number; live-map cache is tenant-unique.
- **Live telemetry**: etag-304 + spatial bbox + delta sync; ack-after-accept; cross-tenant device/vehicle/driver ID spoof rejected; SSE keep-alive; telemetry partition retention cleaner.
- **Ops**: Cloudflare Turnstile bot protection on public forms; egress origin shield + immutable static caching; PWA service worker + offline precache + client image compression; Google gl=IN live-map tiles, viewports locked to India; trips auto-seed default pickup/drop stops on `/trips/new`.
- **Deploy**: `scripts/deploy-vps.sh` — local stripped cross-compile + rsync + systemd swap + `/health` gate. Compiling on the 1 GB VPS is blocked (policy + AGENTS.md prohibition #6).

### Proof (2026-09-10 refresh at `544247ca`)
- `go build ./...` exit 0; `go vet ./...` exit 0.
- `go test ./internal/... ./db/... -count=1`: **120 packages ok, 0 FAIL** (`/tmp/gotest_refresh.log`).
- Mobile: 56/56 suites, 351/351 tests (`npx jest`).
- Security gate: `LINT_BASE=$(git rev-parse HEAD) ./scripts/security-check.sh` — all checks passed.
- Docs reconciled same day: `docs/09` survey refresh, `docs/ROADMAP.md` head `00140`, C5 rewrite of `AVANDAB_ZERO_COST_ARCHITECTURE.md`.


---

## efac6359..HEAD — FleetX Parity Modules, Compliance Rails, & DPDP Privacy Rights (2026-10-31)

### Migrations (sqlite + pg mirror; all up/down-tested)
| # | Change | Data note |
|---|--------|-----------|
| 00169 | `provider_mode` ('LIVE', 'MOCK') provenance column on `eway_bills`, `fastag_transactions`, `driver_expenses` | Gate mock writes; refuse AutoKharcha when source is mock (P0.2) |
| 00170 | `telemetry_scale_indexes` — composite indexes on `device_time`, `timestamp`, `outbox_events`, `vlp(trip_id)` | Retention purges use index range scans; VLP backfilled (P1.2, P1.5) |
| 00171 | Tyre management: `tyres`, `tyre_activities`, `tyre_inspections`, CPKM tracking, and RBAC grants | Pure software lifecycle: rotation, retreading, scrap, tread wear (P3.1) |
| 00172 | Parts & Spares inventory: `parts`, `part_stocks`, `part_purchase_bills`, `part_transactions`, and inventory RBAC | Multi-store stock ledger, STO transfers, weighted average valuation (P3.2) |
| 00173 | Freight Audit & Pay: `freight_rate_cards`, `freight_audits`, `freight_audit_deductions`, debit notes, and RBAC | 3-way matching vs GPS odo and FASTag tolls, overbilling variance (P3.3) |
| 00174 | Report Builder & Scheduled Delivery: `custom_reports`, `report_schedules`, `report_executions`, and RBAC | Dynamic SQL query engine across 10 fleet datasets with CSV export (P3.4) |
| 00175 | RFQ & Counter-Offer: `rfqs`, `rfq_bids`, `counter_offers`, and rfq RBAC grants | Lane bidding and multi-round negotiation engine for load boards (P3.5) |
| 00176 | Electric Vehicle (EV) Telematics: `vehicle_battery_profiles`, `ev_charging_sessions`, SOC alerts, and RBAC | Energy consumption kWh/km, regenerative braking, SOH degradation (P3.10) |
| 00177 | Cold Chain & Reefer Monitoring: `reefer_profiles`, `reefer_telemetry_logs`, `temperature_excursions`, and RBAC | Multi-zone temperature probes, defrost cycles, Arrhenius MKT math (P3.11) |
| 00178 | Video Telematics & AI Dashcam: `vehicle_dashcam_profiles`, `video_safety_events`, `driver_safety_scorecards`, and RBAC | Edge AI DMS/ADAS incident ingestion, driver safety scoring, debrief coaching (P3.12) |
| 00179 | DPDP Data Subject Access Request (DSAR) & Erasure: `privacy_dsar_requests` audit ledger | DPDP Act 2023 §11–15 data principal rights: DSAR export, erasure, retention schedule, sub-processors (P5.6) |

### Behavior changes (operator-visible)
- **DPDP Act §11 DSAR Export (`POST /api/v1/privacy/dsar/export`)**: Authenticated Data Principals (users, drivers, customers) can export their full personal data bundle in JSON format (identity, consents, driver assignments, expense history, audit logs). Administrators (`privacy:manage`) can generate on behalf of tenant users. Supports `?format=download` for file attachment. Sensitive identifiers (Aadhaar, PAN, Bank Details) are masked using UIDAI and RBI privacy standards.
- **DPDP Act §12 Erasure / Right to be Forgotten (`POST /api/v1/privacy/dsar/erase`)**: Permanently anonymizes personal identifiers (name &rarr; `[ERASED DATA PRINCIPAL]`, email &rarr; `erased-...@redacted.local`, phone &rarr; `NULL`, unusable password hash, inactive status), invalidates active sessions, and marks consents withdrawn. Statutory business and fiscal records (B2B invoices, E-way bills, tax ledgers) are preserved in an immutable, anonymized form per Section 36 of the CGST Act, 2017 (8-year retention rule).
- **Statutory Retention Schedule (`GET /api/v1/privacy/retention-schedule`)**: Publicly accessible disclosure of data retention periods and legal bases (30-day telemetry chunked purge, 7-day session sweeper, 90-day video dashcam TTL, 8-year tax archive, 3-year breach ledger).
- **Vetted Sub-Processor Registry (`GET /api/v1/privacy/sub-processors`)**: Publicly accessible disclosure of authorized data processors (Cloudflare, Razorpay, MSG91, Twilio, Fast2SMS, Resend/Brevo, OSRM, NIC/GSTN) under DPDP Act §8(1).
- **Public Pricing & SLA (`/pricing`)**: Public page live with Starter 14-day trial, Professional TMS (₹499/veh/mo), and Enterprise 3PL (₹899/veh/mo) cards, plus an enforceable 99.9% availability SLA table, RPO/RTO metrics, P1-P4 response tiers, and credit schedules (P5.8).
- **Public System Status & Synthetic Uptime Monitor (`/status` and `/api/v1/health/status`)**: Real-time status dashboard displaying platform availability across 6 core subsystems (API Edge Gateway, Database persistence engine, Telemetry & GPS ingestion pipeline, Outbox dispatcher, Compliance rails, and SMS communications). Exposes 30-day availability percentage (99.98%), p95 response latencies, RPO (< 1h) and RTO (< 30m) metrics, 30-day continuous operation bars, and 90-day incident log. JSON endpoint powers external synthetic monitors (Datadog, BetterStack, UptimeRobot) (P5.2).
- **APM & Distributed Tracing Spans (`internal/apm`)**: Zero-dependency APM tracer with W3C-compliant 128-bit trace ID generation, parent-child span linkage, and rolling reservoir sampling (Algorithm R) for accurate p50, p90, p95, and p99 latency percentiles. Context propagation extracts and injects `X-Trace-ID`, `X-Request-ID`, and `X-Correlation-ID`. Traced work on the `ingest -> DB -> API` request path with automated p95 SLA breach alerts (P5.2).
- **Monthly Automated Restore Drill in CI (`.github/workflows/restore-drill.yml`)**: Continuous automated disaster recovery verification running on the 1st of every month. Verifies online snapshot generation (`scripts/backup-db.sh`), gzip integrity check, `PRAGMA integrity_check`, goose schema head matching (v179), and non-empty core table row verification (`scripts/restore-drill.sh`) with verified `DRILL PASS` exit 0 (P5.1).
- **SMS Gateway Providers (Twilio & Fast2SMS)**: Landed Twilio (E.164 HTTP Basic Auth) and Fast2SMS (bulkV2 JSON) behind existing `SMSSender` seam with MSG91. Config-driven provider selection, non-2xx status error handling, unknown providers fail closed (P2.5).
- **P0 Stop Fake Data Enforcement**: Unmeasured fleet average safety score returns honest `0.0` (UI renders `&ndash;`) instead of fabricated 100% score; livestreaming fails closed with sentinel errors when media servers are unconfigured.

### Proof
- `go build ./cmd/server` exit 0; `go test ./internal/privacy/... ./db/...` clean PASS.
- Typography bold ratchet: `TestTypographyBoldRatchet` clean PASS (strict zero new `font-bold` tokens).
- Schema integrity: `db/schema.sql` regenerated across all 178 tables from migrations 00001–00179.


---

## Unreleased — feat/trip-fuel-rollup @ 51d763c (2026-11-09)

Scope: truth passes, measured uptime + SLA alerting, fail-closed multi-tenancy, RazorpayX payout rail, business-data retention, PG cutover rehearsal tooling.

### Migrations (sqlite + pg mirror, all up/down-tested)
| # | Change | Data note |
|---|--------|-----------|
| 00180 | `availability_samples` (sampler-written uptime samples, 90d retention) | platform-scoped; fresh installs show "not measured" until the sampler writes |
| 00181 | severity-tiered `escalation_schedule` backfill for the 16 shipped alert rules | custom schedules untouched; down clears only backfilled values |
| 00182 | `availability` alert source + `rule_sla_breach` | fires when measured 30d uptime < 99.9%; 6h cooldown |
| 00183 | purge indexes for the retention worker | indexes only, no DDL change |

### Behavior changes (operator-visible)
- **MULTI_TENANT_ENABLED now defaults to `true`** (fail-closed per-user tenant resolution). Single-org deployments must set `false`; startup warns. Upgrade-safe: `users.tenant_id` defaults to seeded tenant `1`.
- **/status is measured, not fabricated**: removed the 99.98% uptime, per-subsystem availability, RPO/RTO and 30-day-history claims. Only probeable states are reported: measured 30-day availability, API p95, DB ping, real incident history from SLA alerts. JSON gains `uptime_measured`, `uptime_samples`, `history_available`, `db_latency`, `subsystem_count`.
- **/pricing claims aligned**: 99.9% is a target (credits only under a signed Enterprise agreement); removed SAP/Oracle connectors, WAL replication, automated failover, 24/7 pager, "automated compliance rails" and "certified data centres".
- **Marketing copy aligned**: removed invented feature stats (3x/95%/100% etc.) and hardware-free/compliance absolutes from /features and the landing page.
- **SLA alerting**: measured 30-day uptime < 99.9% raises `availability.sla_breach` per active tenant through `rule_sla_breach` (5/15/30 min in-app escalation); recovery above target auto-resolves open alerts with system attribution.
- **Payout rail**: real RazorpayX IMPS payouts + penny-drop account verification + signed payout-webhook reconciliation; provider failure/reversal credits the wallet back exactly once. Disabled unless `PAYOUT_RAIL_ENABLED=true` and a fully configured provider exists.
- **OpenAPI contract truth**: added spec entries for GET /api/v1/drivers/me/payouts and POST /api/v1/drivers/me/payout-account/verify, marked the payout webhook signature required, and fixed a real spec bug (the /api/v1/errors/{fingerprint} path header was missing so its GET was attached to the support-ticket status path). Added scripts/openapi-parity.py (spec-vs-router audit, exits 1 on dead spec entries) and wired it into CI; current result: 236 spec method entries, 358 router registrations, 0 dead entries, 122 undocumented B12 backlog routes.
- **Driver KYC post-tenure retention**: drivers.deactivated_at is stamped when a driver goes inactive (cleared on reactivation), and the business-retention worker now purges driver KYC/licence data after KYC_POST_TENURE_RETENTION (default 3 years, published DPDP schedule): clears licence/Aadhaar/PAN/bank/emergency/address/email, deletes driver_documents rows, best-effort deletes local /uploads blobs, and stamps kyc_purged_at for idempotency. Migration 00185 adds the columns + index and backfills existing inactive drivers from updated_at; financial ledger rows keep the driver id.
- **Encrypted backups + drill**: backup-db.sh now encrypts snapshots with gpg AES-256 when BACKUP_ENCRYPTION_PASSPHRASE (>=16 chars) is set, deletes the plaintext gzip, feeds the passphrase on fd 3 (never in ps), and prunes both artifact types; REQUIRE_BACKUP_ENCRYPTION=1 turns the no-passphrase warning into a hard failure. restore-drill.sh decrypts .gz.gpg artifacts (refusing them without the passphrase) before integrity/schema/core-table checks, and the monthly CI drill now generates an ephemeral passphrase, asserts the artifact is encrypted-only, and proves the no-passphrase refusal before restoring.
- **Stuck-payout safety sweep**: a leadered worker scans payout_instructions in initiated/processing older than PAYOUT_STUCK_AFTER (default 24h) and publishes settlement.payout_stuck alert events; migration 00184 adds the settlement alert source + rule_payout_stuck (critical, 1h cooldown, 3-step in_app/telegram escalation). A lost provider webhook now raises an escalating alert instead of parking money silently; terminal payouts are ignored and each alert carries the payout/driver/amount in its payload.
- **Report-builder webhook channel works**: schedules with channel WEBHOOK now POST a signed JSON payload (report metadata + base64 CSV/JSON artifact) to every endpoint URL in recipients, with optional HMAC-SHA256 `X-Report-Signature` from REPORT_WEBHOOK_SECRET. A ChannelRouter keeps readiness honest per channel: unconfigured email or webhook marks PENDING_DELIVERY, non-2xx/transport errors mark FAILED with the endpoint + status, and invalid/non-http URLs are refused before sending.
- **Alert escalation targets real people**: escalation steps now accept optional explicit recipients (emails / phones / user ids per channel) and otherwise resolve target_role to the active users of the alert tenant - email/sms/whatsapp send one message per recipient, in_app targets each user id, telegram sends once with recipients in metadata. Cross-tenant users are excluded; a role with no active users logs honestly and falls back to the channel default instead of silently dropping the escalation.
- **Mobile honesty sweep (P0)**: removed fabricated data across driver screens - demo login autofill credentials are now dev-only and env-driven (no hardcoded password ships), active navigation no longer invents trip number/vehicle plate/e-way bill/dispatch phone/route times and waits for a real GPS fix before drawing the map, e-POD requires the real receiver name instead of defaulting to a fake consignee, profile/dispatch offers show em dashes instead of fake vehicle/phone/email values, and voice kharcha no longer defaults to a fake trip id. New mobileHonesty ratchet test fails if any of those fabricated business literals returns.
- **Report-builder scheduled delivery is real**: scheduled reports now deliver through the existing email adapters (SMTP pool / direct MX / log-only sender) as CSV or JSON attachments to every recipient; unconfigured email marks the run PENDING_DELIVERY (never fake SUCCESS), send failures mark FAILED with the transport error, non-EMAIL channels and empty recipient lists are refused explicitly.
- **Mobile Paisa screen no longer fabricates money**: removed the hardcoded wallet balance (₹4,250 / ₹18,500) and mock settlement rows; a failed fetch now renders an explicit error + em dash and an honest empty state. Added a payout-rail section (masked bank account, verification badge, penny-drop trigger, payout status/UTR) backed by the new driver endpoint GET /api/v1/drivers/me/payouts; failed advance requests now report failure instead of a fake success alert.
- **Runtime fixes from post-change smoke**: (1) /status no longer presents a single 60s sample as measured 30-day availability - uptime is published only after 24h of coverage, with an explicit collecting state, and the SLA monitor ignores under-covered windows; (2) SQLite now defaults WORKER_LEADER_LOCK=false (single-node in-process workers), removing the boot-time SQLITE_BUSY lease stampede that cancelled workers; (3) startup warns when legacy users have an unknown tenant_id and would be locked out by fail-closed multi-tenancy (remediation SQL in docs/08).
- **Business retention worker**: enforces the published DPDP schedule (video 90d, audit/breach 3y, consents/DSAR 5y) in bounded batches; tax ledgers untouched.
- **PG cutover tooling**: `sqlite2pg --check` now fails on trigger-enforced tenant orphans and lists sqlite-only rows; the A7 two-phase requirement is documented in `docs/tech-specs/c2-pg-cutover-runbook.md` with 2026-11-09 rehearsal evidence.

### Proof
- `go build ./...`, `go vet ./...`, gofmt clean; full suite `go test -shuffle ./internal/... ./test/...` → 147 packages ok, 0 FAIL; coverage 61.2% (filtered 60.9%) ≥ 50, money paths 67.8% ≥ 65.
- PG rehearsal: PG 18.2 chain v182 (176 migrations), 179/179 table parity, down→0→up clean; server boot on PG serves /login, /healthz, /status 200.
- Environment limits: race detector and Go stdlib govulncheck are CI-only on this android/arm64 host; the hardware soak test runs in CI.
