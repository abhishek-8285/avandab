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
- **SMS Gateway Providers (Twilio & Fast2SMS)**: Landed Twilio (E.164 HTTP Basic Auth) and Fast2SMS (bulkV2 JSON) behind existing `SMSSender` seam with MSG91. Config-driven provider selection, non-2xx status error handling, unknown providers fail closed (P2.5).
- **P0 Stop Fake Data Enforcement**: Unmeasured fleet average safety score returns honest `0.0` (UI renders `&ndash;`) instead of fabricated 100% score; livestreaming fails closed with sentinel errors when media servers are unconfigured.

### Proof
- `go build ./cmd/server` exit 0; `go test ./internal/privacy/... ./db/...` clean PASS.
- Typography bold ratchet: `TestTypographyBoldRatchet` clean PASS (strict zero new `font-bold` tokens).
- Schema integrity: `db/schema.sql` regenerated across all 178 tables from migrations 00001–00179.
