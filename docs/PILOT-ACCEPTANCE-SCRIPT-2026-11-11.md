# Pilot Acceptance Script (2026-11-11)

> Companion to `docs/PLAN-product-details-2026-11-11.md` §3-I and §7.
> Derived from `docs/09` §4 feature inventory, `docs/PLAN-fleetx-parity` P0/P5,
> `docs/tech-specs/c4-provider-golive-runbook.md`, `c2-pg-cutover-runbook.md`.
>
> Runs against a dedicated pilot tenant, not a demo tenant. Every check needs
> evidence (screenshot, HTTP status/body, log line, or query result). "Looks
> fine" is not evidence.
>
> Do not claim a module in pilot scope unless its row is PASS with evidence.

## 0. Pilot setup (fill before running)

| Item | Value |
|---|---|
| Pilot tenant / org name | |
| Segment / fleet size | |
| Vehicles (count + plates) | |
| Drivers (count + phones) | |
| Users + roles (org_admin, ops, accounts, driver) | |
| Customers / consignees | |
| Routes / depots / facilities | |
| Devices (model + protocol GT06/AIS-140) | |
| Modules in scope (check) | booking, trip, dispatch, POD, billing, payments, kharcha, compliance, fleet, reports, mobile, AI |
| Provider modes | e-way bill: mock/live; GSTN: mock/live; FASTag: mock/live; payouts: on/off; SMS: mock/live; email: real |
| Start date / duration | |
| Run by / approved by | |

## 1. Environments and gates

| Env | Use | Destructive checks allowed |
|---|---|---|
| STAGING | setup, restore drill, rollback, tenant purge, provider sandbox | yes |
| PILOT (prod data) | business flow, telemetry, mobile, invoices | no |
| PROD | read-only smoke only | no |

Preconditions: `APP_ENV=production` posture on pilot; `MULTI_TENANT_ENABLED=true`;
`AGENT_REQUIRE_APPROVAL=true`; backup job green; `/health` 200; migration head
matches `db/migrations`; mock flags off for every integration claimed in the
pilot; approval-gated actions enabled for agent mutations.

## 2. Severity

| Sev | Definition | Pilot gate |
|---|---|---|
| Sev1 | Data loss/corruption, cross-tenant exposure, money movement wrong, telemetry loss >1 min, crash | blocks pilot |
| Sev2 | Core flow broken or workaround needed, invoice/tax wrong, auth/RBAC bypass | fix before pilot exit |
| Sev3 | Cosmetic, non-core module, documentation mismatch | accepted with owner + date |

## 3. Checks

### A. Setup, tenants, access

| ID | Env | Check | Pass criteria | Evidence |
|---|---|---|---|---|
| A-01 | PILOT | Provision pilot tenant; log in as org_admin | Tenant isolated; dashboard loads; no other tenant data | Screenshot + tenant id |
| A-02 | PILOT | Create users for ops/accounts/driver; verify role gates | Each role sees only permitted pages/APIs; 403 on forbidden | Screenshot/HTTP |
| A-03 | PILOT | Login paths: password, Google OAuth (if enabled), email verify badge | All work; unverified state visible; no open redirect | Screenshot |
| A-04 | PILOT | Trial/entitlement state visible; quota counter changes on booking create | Quota decrements; over-quota refused with honest error | Screenshot + API |
| A-05 | STAGING | Attempt tenant switch/bypass via URL/API param | 401/404; no cross-tenant read | HTTP codes |

### B. Telemetry ingest

| ID | Env | Check | Pass criteria | Evidence |
|---|---|---|---|---|
| T-01 | PILOT | Send valid GT06 frame to TCP `:5023` for a provisioned device | Frame stored once; live map shows vehicle at correct coords | Ingest log + map |
| T-02 | PILOT | Send AIS-140 frame; corrupt checksum variant | Valid accepted, corrupt rejected; no silent bad row | Log + query |
| T-03 | PILOT | Duplicate frame replay | Deduped; latest position unchanged; no double trip event | Query |
| T-04 | PILOT | Telemetry clock: send frame with device time; verify `ts_unix` ordering | Readers use canonical epoch; no row dropped by text timestamp | Query + map |
| T-05 | PILOT | Stop device 10 min | Vehicle shows stale/offline honestly; no fabricated movement | Screenshot |
| T-06 | PILOT | Geofence enter/exit around a facility | Zone events fire once; dwell event; no duplicates on retry | Query zone events |
| T-07 | PILOT | Live map bbox/etag/delta sync and SSE updates | No cross-tenant points; update delta only | Network capture |
| T-08 | STAGING | Quarantine/reconcile path for malformed payload | Quarantined, visible, replayable; no business-table write | Query + log |

### C. Booking → trip → delivery

| ID | Env | Check | Pass criteria | Evidence |
|---|---|---|---|---|
| B-01 | PILOT | Create booking (customer, route, freight, stops) | Booking created with tenant + idempotency; number unique | API + screenshot |
| B-02 | PILOT | Duplicate create with same idempotency key | One booking only | API |
| B-03 | PILOT | Dispatch: assign vehicle/driver; offer/quote if enabled | Assignment respects compliance/maintenance blocks | Screenshot |
| B-04 | PILOT | Start trip; verifies start odometer/geofence rule | State `in_progress`; start event; no double start | Query trip |
| B-05 | PILOT | Arrive/depart stops; dwell triggers detention as configured | Detention billing event computed once | Query + invoice draft |
| B-06 | PILOT | Delivered + close odometer; breakdown path | Delivered state; close odometer recorded; breakdown alert | Screenshot/query |
| B-07 | PILOT | Cancel trip/booking | Correct reversal; no orphan invoice | Query |
| B-08 | PILOT | EWB multi-leg/multistop generate (if live) or mock-labelled | Correct provider mode label; no mock id in business table | Query + screen |
| B-09 | PILOT | EWB extend/cancel lifecycle | Validity/events/audit correct | Query |
| B-10 | PILOT | Share link for customer tracking | Shows milestones; no PII leakage (masked phone); TTL applies | Screenshot |

### D. POD / ePOD

| ID | Env | Check | Pass criteria | Evidence |
|---|---|---|---|---|
| P-01 | PILOT | Driver uploads POD + OTP verification | Artifact stored tenant-scoped; OTP single-use; mismatch rejected | Screenshot/query |
| P-02 | PILOT | Driver completes POD offline, syncs later | Queued then synced exactly once | Log + query |
| P-03 | PILOT | Admin views POD | Visible with correct trip; no cross-tenant file read | Screenshot |
| P-04 | STAGING | Delete/replace artifact | Audit trail; no orphaned blob queryable | Query |

### E. Billing, GST, invoices

| ID | Env | Check | Pass criteria | Evidence |
|---|---|---|---|---|
| I-01 | PILOT | Generate invoice from delivered trip | Line math correct; tax computed by engine | PDF + API |
| I-02 | PILOT | CGST/SGST vs IGST case (intra/inter-state) | Correct split; place of supply respected | Invoice PDF |
| I-03 | PILOT | Rounding edge cases (3 lines) | Totals reconcile to paise; no float drift | API |
| I-04 | PILOT | Credit note for dispute | Reversal tied to invoice; ledger consistent | Screenshot/query |
| I-05 | PILOT | PDF + UPI QR render | Amount/UPI correct; seller entity present | PDF |
| I-06 | PILOT | GSTIN checksum validation on onboarding | Invalid GSTIN rejected | Screenshot |
| I-07 | PILOT | Invoice list/aging reflects status | Paid/outstanding accurate after payment | Screenshot |
| I-08 | PILOT | IRN/e-invoice (only if live) | IRN + signed QR generated; else mock clearly labelled and not persisted as real | Query |

### F. Payments, ledger, payouts

| ID | Env | Check | Pass criteria | Evidence |
|---|---|---|---|---|
| M-01 | PILOT | Customer pays invoice via Razorpay | Order + verify + webhook; invoice marked paid once | API + screen |
| M-02 | PILOT | Webhook replay/forgery | Signature required; replay idempotent; forged rejected | HTTP + query |
| M-03 | PILOT | Payment reconciliation into ledger | Double-entry correct; no duplicate credit | Query |
| M-04 | PILOT | Driver settlement/wallet view | Balance matches ledger; no fabricated numbers | Screenshot |
| M-05 | STAGING | RazorpayX sandbox ₹1 payout + penny-drop | Payout instruction + debit in one unit; webhook reconciles | API + query |
| M-06 | STAGING | Provider failure/reversal | Compensation exactly once; wallet restored; alert | Log + query |
| M-07 | STAGING | Stuck-payout sweep | Alert fires past threshold; no double payout | Log |
| M-08 | STAGING | Payout rail disabled default | InitiatePayout fails closed; no ledger debit | API |

### G. Kharcha (driver expenses)

| ID | Env | Check | Pass criteria | Evidence |
|---|---|---|---|---|
| K-01 | PILOT | Driver submits expense (cash/fuel) | Pending state; tenant-scoped; OCR fields nullable | Screenshot |
| K-02 | PILOT | OCR mock/live behavior | Mock never auto-approves; live mismatch → manual review | Query |
| K-03 | PILOT | Admin approve/reject | Ledger updated; audit; driver notified | Screenshot |
| K-04 | PILOT | Fuel-audit enforcement (odometer/consumption) | Anomaly flagged, not silently approved | Query |
| K-05 | PILOT | Duplicate submission | Dedup/flag; no double payment | Query |

### H. Compliance & privacy

| ID | Env | Check | Pass criteria | Evidence |
|---|---|---|---|---|
| C-01 | PILOT | Vehicle document expiry hard-block | Expired doc blocks assignment with clear reason | Screenshot |
| C-02 | PILOT | Driver licence expiry gate | Blocks with reason; unknown dates allowed honestly | Screenshot |
| C-03 | PILOT | Consent capture at registration | Explicit unchecked box; server refuses without consent | Screenshot |
| C-04 | STAGING | DSAR export + erasure | Export masked; erasure invalidates sessions; tax records preserved | API + query |
| C-05 | STAGING | Retention worker dry-run | Purges only past schedule; no live record lost | Log + counts |
| C-06 | STAGING | KYC post-tenure purge | Purge only after 3y; financial ledger preserved | Query |

### I. Fleet, maintenance, reports

| ID | Env | Check | Pass criteria | Evidence |
|---|---|---|---|---|
| F-01 | PILOT | Work order create → complete | Service record + due flag resolve; dispatch block clears | Screenshot |
| F-02 | PILOT | Maintenance plan call → auto job card | Job card generated at horizon; no duplicate | Query |
| F-03 | PILOT | Tyre/inventory/fuel-card headless modules | Real usage reflected; no mock defaults | Screenshot |
| F-04 | PILOT | Gate register / KMPL / ZMOTM report export | CSV matches source rows; formulas correct | CSV |
| F-05 | PILOT | Scheduled report email/webhook | Real delivery; PENDING_DELIVERY when unconfigured (never fake SUCCESS) | Mailbox/log |
| F-06 | PILOT | ESG snapshot/report (if in scope) | Fuel/tonne-km fallback honest; no invented defaults | Report |

### J. Mobile app

| ID | Env | Check | Pass criteria | Evidence |
|---|---|---|---|---|
| D-01 | PILOT | Login + role home (driver/manager) | Correct mode; no fake KPIs | Screenshot |
| D-02 | PILOT | Offline: create kharcha/POD/trip close in airplane mode, reconnect | Queues persist; sync exactly once; conflict visible | Log + server |
| D-03 | PILOT | Live navigation/map | Vehicle position real; no hardcoded route/EWB data | Screenshot |
| D-04 | PILOT | SOS trigger | Alert pipeline + cooldown; no duplicate alerts | Screenshot/query |
| D-05 | PILOT | Push notification (if FCM live) | Delivered; token registered; fallback visible if unconfigured | Device |
| D-06 | PILOT | Release build (no cleartext, no debug creds) | Release config confirmed; no demo login | Build artifact |

### K. AI assistant

| ID | Env | Check | Pass criteria | Evidence |
|---|---|---|---|---|
| G-01 | PILOT | Ask read query ("open alerts", "today's trips") | Correct tenant data; no invented numbers | Screenshot |
| G-02 | PILOT | Ask mutating action (create booking / record payment) | Pending action created, not executed | Screenshot |
| G-03 | PILOT | Approve action at `/agent-actions` | Executes under admin identity; audit + reward logged | Query |
| G-04 | PILOT | Reject action | No mutation; decision logged | Query |
| G-05 | PILOT | RAG/support answer with citations | Cites knowledge; refuses when RAG disabled/mock | Screenshot |

### L. Security & tenancy (must pass or pilot is red)

| ID | Env | Check | Pass criteria | Evidence |
|---|---|---|---|---|
| X-01 | PILOT | Cross-tenant read attempts on trips/invoices/files/EWB | 404/empty; no data leak; may be indistinguishable from unknown id | HTTP |
| X-02 | PILOT | Missing tenant context (API) | 401, never default tenant fallback | HTTP |
| X-03 | PILOT | `/metrics` unauthenticated | 401 unless loopback/token | HTTP |
| X-04 | PILOT | Mock provider mode with flags on | Fail closed or labelled mock; no mock ids in business tables | Query |
| X-05 | PILOT | Public payment link/POD file access by guessing ids | Capability rules hold; no enumeration | HTTP |
| X-06 | STAGING | Tenant purge preview/execute | Preview no-op; execute guarded, bootstrap protected, audit written | API |
| X-07 | STAGING | Secret scan / no default secrets | App refuses known defaults; no plaintext keys in repo | Startup log |

### M. Ops & trust surface

| ID | Env | Check | Pass criteria | Evidence |
|---|---|---|---|---|
| O-01 | PILOT | `/health` and `/status` | Measured uptime (not hardcoded); incidents honest | Screenshot |
| O-02 | PILOT | APM p95 thresholds + structured warnings | Breach emits warn (no flood); metrics count visible | Log |
| O-03 | STAGING | Encrypted backup + restore drill | Restore verifies; monthly drill green; passphrase required | Job log |
| O-04 | STAGING | Rollback rehearsal | Previous binary restored, health green, <120s | Log |
| O-05 | PILOT | SLA alert path (force breach in staging data) | Alert fires; recipient targeting correct; auto-resolve on recovery | Log |
| O-06 | PILOT | Incident/status page comms (manual dry run) | Runbook followed; status page updated | Notes |

## 4. Exit criteria

1. All Sev1 checks PASS with evidence; zero open Sev1.
2. Sev2 fixed or explicitly accepted by sign-off owner with date.
3. Pilot data: every check marked PASS/ACCEPTED/PENDING — no blanks.
4. Evidence archive linked (folder path) and referenced in this script's copy.
5. Product claims used in sales (pricing, SLA, compliance, integrations) each
   map to a PASS row or are removed from claims.
6. Ops readiness: on-call owner named, backup/restore proven, rollback proven,
   incident contact published.

## 5. Sign-off

| Role | Name | Scope signed | Date |
|---|---|---|---|
| Founder | | pilot + commercial claims | |
| Product | | module scope + UX | |
| Engineering | | technical checks, security | |
| Ops | | telemetry, backups, monitoring | |
| Pilot customer | | business acceptance | |
