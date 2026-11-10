# 09 — Codebase & Feature Assessment

> Evidence-based snapshot of built state, quality, and honest gaps. Survey date: **2026-11-10**, commit `41345ca` (branch `feat/trip-fuel-rollup`, 29 commits ahead of `origin`, clean tree). Supersedes the `544247ca` (2026-09-10), `3f015a8b` (2026-09-08) and `ea5b078` surveys; deltas called out per section. `docs/*.md` supersede on conflict (per `docs/ROADMAP.md` header rule).

---

## 1. Measured metrics

| Metric | Value | Source |
|---|---|---|
| Non-test Go LOC (`internal/` + `cmd/`) | 148.7k / 646 Go files | `find internal cmd -name '*.go' ! -name '*_test.go' \| xargs wc -l` |
| Test LOC / test files (Go) | 100.9k / 516 `_test.go` files (~68% test:code ratio) | same method |
| Test result (Go) | 148 packages `ok`, **0 FAIL**, exit 0 | `go test -shuffle ./internal/... ./test/...` 2026-11-10 (`final-suite.log`) |
| Go coverage | **61.2%** total / 60.9% filtered (floor 50); money paths **67.3%** (floor 65) | `go tool cover` + `scripts/check-coverage.sh` floors |
| Mobile tests | **69 suites / 428 tests**, all pass | `jest` 2026-11-10 |
| Mobile coverage | stmt 85.56% / **branch 76.31%** / func 85% / line 88.09% (branch floor 75% — thin margin) | `jest --coverage` 2026-11-10 |
| Build / vet / gofmt | exit 0 / exit 0 / clean (tracked Go) | `go build ./...`, `go vet ./...`, `gofmt -l` 2026-11-10 |
| Security gate | **Partial locally** — golangci-lint 0 issues across all branch changes; `govulncheck` + `-race` are CI-only on this android/arm64 host | `golangci-lint --new-from-rev=origin` 2026-11-10 |
| Migrations | **179 files each engine, head `00185`**, SQLite + PG mirrors | `ls db/migrations*/ *.sql` |
| Schema | 179 tables / 283 indexes | regenerated `db/schema.sql` at head |
| Internal packages | 79 top-level / 200 Go packages under `./internal/...` | `go list` |
| TODO/FIXME/HACK markers in `internal/` + `cmd/` | **0** | `grep -rn 'TODO\|FIXME\|HACK' --include='*.go'` |
| Packages without tests | 95 of 200 (`./internal/...`) | `go list -f` 2026-11-10 |
| OpenAPI | 206 hand-maintained paths (`openapi.yaml`); new driver payout routes not all spec'd | `grep -cE '^  /' openapi.yaml` |
| CI | 8 workflows; aggregate gate + backend build/test, security, mobile, PG parity, Playwright, deploy, restore drill, perf/mutation | `.github/workflows/` |
| PG rehearsal | **v185**, PG 18.2: 179/179 table parity, down→0→up clean, `-tags pg_integration` green, PG boot smokes 200 | `docs/tech-specs/c2-pg-cutover-runbook.md` |

### Deltas since `544247ca` (2026-09-10 → 2026-11-10)
- **Trust surface truth pass**: `/status` is measured (availability sampler, real incident history, coverage-gated 30-day uptime — no 99.98% invention), `/pricing` 99.9% is a target, marketing copy de-fabricated; SLA breach alerting (00182) + auto-resolve on recovery; alert escalation defaults (00181) and **recipient targeting** (role → active tenant users, explicit recipients, cross-tenant excluded).
- **Money movement**: real **RazorpayX payout rail** (IMPS, penny-drop verification, signed webhook reconciliation, exactly-once wallet compensation), driver + admin payout UI, and a **stuck-payout sweep** (00184) that alerts when a payout sits in initiated/processing past threshold.
- **Privacy/retention**: business-data retention worker (00183 indexes; video 90d, audit/breach 3y, consents/DSAR 5y) and **driver KYC post-tenure purge** (00185: deactivated_at/kyc_purged_at, KYC fields + documents + local blobs purged after 3y, financial ledger preserved).
- **Reports/ops**: scheduled report delivery now real — email (SMTP pool/direct MX) and **webhook** (signed JSON + artifact, per-channel honest PENDING_DELIVERY/FAILED); backups are **AES-256 encrypted** with a decrypt-verifying monthly drill.
- **PG readiness**: two-phase migration helper + identity resync, `sqlite2pg --check` tenant-orphan gate, full PG 18.2 rehearsal at v185.
- **Mobile honesty (P0)**: removed fabricated Paisa balance/settlements, demo login credentials, fake nav/EWB/dispatch data, fake e-POD consignee defaults; added a source ratchet test.
- Multi-tenancy now **fail-closed by default** (`MULTI_TENANT_ENABLED=true`); legacy orphan-tenant users warn at boot with remediation SQL.

## 2. What is genuinely strong

1. **Testing is real, not vanity.** 148 Go packages green (0 FAIL) with coverage floors enforced (61.2% total, 67.3% money paths); 69 mobile suites / 428 tests green; ratchet tests pin typography, mock honesty, tenant isolation, locale parity, migration parity, mobile fake-data.
2. **Hygiene discipline.** 0 TODO/FIXME/HACK markers in 148.7k LOC; tenant lint at 0 warnings; security lint ratcheted to changed code.
3. **Migration governance.** 179-file append-only dual-engine history with ownership index and reserved-slot discipline; head `00185` rehearsed on real PG 18.2 (up/down/re-up, 179/179 parity).
4. **Docs do not lie.** Roadmap/RELEASES/retention schedule self-report mock providers, pending creds, single-node limits; this survey supersedes stale claims (mock OCR ₹3000, thin OpenAPI, 99.98% status, 134 migrations).
5. **DDD islands done right.** `internal/booking/` and the new settlement payout seam (`internal/settlement/payout`, `monitor`) follow `docs/CONTRIBUTING.md` boundaries; domain does not import `net/http`/`database/sql`.
6. **Money safety is designed, not hoped.** Payout debit + instruction are one unit, provider failure/reversal compensates exactly once, webhook signature mandatory, stuck payouts escalate, KYC purge is idempotent, backups encrypted.

## 3. Honest weaknesses

### 3.1 Two architectures coexist (highest structural risk — grew)
- `internal/handlers/` god-files grew: `trips.go` 2,034, `app.go` 1,764, `invoices_consolidate.go` 1,673, `invoices.go` 1,258; `internal/agent/tools.go` 1,185 keeps the pattern alive in a new directory.
- **End-state now decided**: `docs/tech-specs/handlers-end-state-adr.md` (2026-11-10) sets the target boundary (handlers/ = HTTP+composition only, zero SQL/business rules; agent tools delegate to application services), a phased migration order, exit criteria, and a CI ratchet (`internal/architecture/ratchet_test.go`: 245 handler SQL sites, per-file caps, god-file line caps may only shrink). Score stays 6/10 until execution reduces those caps.

### 3.2 Production readiness gaps (external acceptance, not code)
| Area | State | Proof / blocker |
|---|---|---|
| RazorpayX payouts | Code complete (rail, penny-drop, webhook, UI); **live proof missing** | needs `RAZORPAYX_*` + `PAYOUT_ENCRYPTION_KEY` creds |
| Provider integrations (EWB, GSTN, FASTag; accounting Zoho/QuickBooks) | **mock-by-default**; Tally has a live adapter | roadmap C4; needs partner certs/creds; accounting Zoho/QB live adapters not implemented |
| PG live cutover | **Rehearsed v185**, production still SQLite | needs freeze window + VPS + prod data; runbook ready |
| Off-host backups | AES-256 encryption + monthly drill proven on synthetic backups | **no real R2 artifact restored into staging** — needs R2 creds |
| SMS / WhatsApp / OCR / FCM / email live delivery | Adapters implemented, fail-closed without creds | needs carrier/BSP/vendor/Firebase/SMTP creds |
| Hardware & FOTA | Absent (P4 business decision) | `vehicle_commands` writes DB, calls nothing; no firmware inventory/jobs |
| OpenAPI coverage | 206 hand-maintained paths; driver payout/webhook additions not fully spec'd | B12 backlog; no generator by design |
| Scale | single 1 GB VPS, no HA/read replica; Timescale deferred (trigger >5k trucks) | roadmap C1/C2 |
| Data integrity | A3 closed 2026-09-08 (`foreign_key_check` 0 rows); not re-run this survey | `scripts/cutover-data-cleanup.sql` |
| AI assistant safety | approval gate + RL loop local-first; depends on prod flag hygiene | AGENTS.md guardrails |
| Telemetry clock seam | `timestamp` TEXT holds unparseable Go-String layouts; `ts_unix` is canonical — new readers using `datetime(timestamp)` silently drop rows | `00134`, docs/02 |

### 3.3 Test coverage is broad; depth still uneven
- Mobile branch coverage 76.31% vs 75% floor — thin margin; next uncovered branch breaks the gate.
- Go: 95 of 200 `./internal/...` packages have no test files (triage scaffolding vs real gaps); coverage depth uneven.
- `-race` and `govulncheck` cannot run on this android/arm64 host — CI is authoritative; hardware soak test also CI-only.

## 4. Feature inventory (built, verified)

- **Core ops:** booking → trip state machine (dwell/detention) → geofence → dispatch offers/quotes → E-way bills incl. multi-leg + delivered lifecycle (00112/00113/00127/00128); STO/load board/backhaul/RFQ/facilities/ESG/fuel cards (00145–00148, 00175).
- **Billing & money:** GST engine, PDF/QR invoices, credit notes, settlement ledger, Razorpay payments; **RazorpayX payout rail** (00111 + payout seam) with penny-drop + webhook reconciliation + stuck-payout alerts (00184).
- **Telemetry (hardened):** GT06/AIS-140 TCP :5023 + MQTT :1883 ingest, provider parity (00117), route/ETA jobs, `ts_unix` clock (00134), SSE tenant isolation, quarantine/reconcile, partition retention cleaner, live map delta sync.
- **Fleet/people:** vehicle compliance hard-block, work orders (00123), maintenance plans (00143), fuel engine/audits, fleet SOP parity (00126), tyre/inventory/freight audit (00171–00173), driver lifecycle (00108), kharcha, scorecards; **driver KYC post-tenure purge** (00185).
- **Platform/trust:** multi-tenancy registry + trigger hardening (00102–00105, fail-closed default), entitlements/subscriptions (00114/00115), company profiles (00125), comm outbox + email pool (00118/00120), leader leases (00079), RAG RBAC (00078), agent orchestrator + RL + approval gate, S3/R2 storage + `files.tenant_id` (00140), PWA/Turnstile, AV teleoperation, **measured status + SLA alerts (00180–00182)**, **alert escalation recipient targeting**, **report delivery email/webhook**, **AES-256 encrypted backups + restore drill**, **business-data retention worker (00183)**.
- **Onboarding:** GSTN checksum gate, fail-hard trial seed, email-verify seam (00130/00133), funnel API, wizard draft, reviewer gating.
- **Deferred by decision:** Timescale/OLAP, ignition-derived trip boundaries, hardware/OEM path, FOTA, white-label accent colours, schema-default cleanups (table-rebuild precedent).

## 5. Scorecard

| Dimension | Score | Basis |
|---|---|---|
| Security & tenancy | 9/10 | fail-closed multi-tenant default, tenant isolation triggers/tests, webhook HMACs, encrypted bank data/backups; full `security-check.sh` is CI-only here |
| Migrations & data governance | 9/10 | 179-file append-only dual-engine chain, head 00185, ownership index, PG 18.2 v185 rehearsal (parity + down/up) |
| Testing | 9/10 | 148 Go packages 0 FAIL + coverage floors (61.2% / money 67.3%), 69 mobile suites/428 tests, ratchets; 95 no-test Go packages and thin mobile branch margin remain |
| Architecture consistency | 6/10 | god-files grew (trips 2,034 / app 1,764 / agent tools 1,185); no end-state ADR |
| Feature depth / prod readiness | 7/10 (was 6) | payout rail, retention (incl KYC), report delivery, encrypted backups, measured trust surface shipped; live provider creds, PG cutover, hardware, off-host restore proof still missing |
| Documentation honesty | 9/10 | roadmap/RELEASES/retention schedule self-report; this refresh replaces stale claims |
| **Overall** | **8.5/10** (was 8) | code-side depth + safety improved materially; external acceptance (creds, VPS, hardware) still gates production |

*(Scores re-derived this survey from the evidence in §1; no re-grading of untouched areas without new evidence.)*

## 6. Recommended order of work

**External unblockers (acceptance, not code):** push auth (29 commits), RazorpayX creds (sandbox payout proof), SMS/OCR/FCM/email creds, R2 creds (off-host restore into staging), VPS freeze window (PG cutover), hardware/OEM decision.

**Then, in order:**
1. **PG live cutover** using the v185-rehearsed runbook (freeze → encrypted backup → `sqlite2pg --check` tenant-orphans=0 → live copy → PG smokes → flip → rollback kept).
2. **Provider go-live per `c4-provider-golive-runbook.md`** — EWB/GSTN/FASTag partner certs; implement Zoho/QuickBooks live adapters (currently mock-only).
3. **Ops acceptance**: verify stuck-payout escalation + alert recipient targeting in a real tenant; reconcile KYC blob `blobs_pending` manual queue.
4. **Architecture end-state ADR** for `handlers/` + `agent/tools.go` god-files before further growth.
5. **OpenAPI spec** for the new driver payout/webhook routes; re-run A6 parity audit.
6. **Test-debt triage**: 95 no-test Go packages; mobile branch margin; keep race/govulncheck/soak in CI.

## 7. Sources

- `/data/data/com.termux/files/home/dsh-verify/final-suite.log` — `go test -shuffle ./internal/... ./test/...`, 2026-11-10 (148 ok / 0 FAIL)
- `/data/data/com.termux/files/home/dsh-verify/mobile-coverage.log` — `jest --coverage`, 2026-11-10
- `go tool cover -func=coverage.out`; `scripts/check-coverage.sh` floors; `golangci-lint --new-from-rev=origin`
- `docs/tech-specs/c2-pg-cutover-runbook.md` (PG 18.2 v185 rehearsal); `docs/RELEASES.md`; `docs/ROADMAP.md`; `docs/PLAN-fleetx-parity-2026-10-28.md`
- Measured on commit `41345ca`; prior surveys `544247ca` (2026-09-10), `3f015a8b` (2026-09-08), `ea5b078`
