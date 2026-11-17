# 09 — Codebase & Feature Assessment

> Evidence-based snapshot of built state, quality, and honest gaps. Refreshed survey date: **2026-11-11**, commit `4a3b07f` (branch `feat/trip-fuel-rollup`). Supersedes the `41345ca` (2026-11-10), `544247ca` (2026-09-10), `3f015a8b` (2026-09-08) and `ea5b078` surveys; deltas called out per section. This refresh records the executed end-state migrations (ADR Phases 2–3 complete) and the first Phase 4 cleanup inventory. Delta **2026-11-17** (`1bc51d5`, PR #49 audit hardening): migration chain extended to `00189` (183 files/engine, 181 tables), `db/schema.sql` regenerated, handlers/ zero-SQL invariant held; measured counts below refreshed where the chain moved. `docs/*.md` supersede on conflict (per `docs/ROADMAP.md` header rule).

---

## 1. Measured metrics

| Metric | Value | Source |
|---|---|---|
| Non-test Go LOC (`internal/` + `cmd/`) | 148.7k / 646 Go files | `find internal cmd -name '*.go' ! -name '*_test.go' \| xargs wc -l` |
| Test LOC / test files (Go) | 100.9k / 516 `_test.go` files (~68% test:code ratio) | same method |
| Test result (Go) | **164 test-bearing packages** (of 226), **0 FAIL** per PR #49 | `go list -f` 2026-11-17; `go test ./...` in CI (`final-suite.log` 2026-11-10: 148 ok at `41345ca`) |
| Go coverage | **61.2%** total / 60.9% filtered (floor 50); money paths **67.3%** (floor 65) | `go tool cover` + `scripts/check-coverage.sh` floors |
| Mobile tests | **68 suites / 426 test cases** (static count; CI `jest`) | `git ls-files mobile/**/*.test.*` + `grep` 2026-11-17 |
| Mobile coverage | stmt 85.56% / **branch 76.31%** / func 85% / line 88.09% (branch floor 75% — thin margin) | `jest --coverage` 2026-11-10 |
| Build / vet / gofmt | exit 0 / exit 0 / clean (tracked Go) | `go build ./...`, `go vet ./...`, `gofmt -l` 2026-11-17 |
| ADR ratchet — `internal/handlers/` raw SQL | **0** sites (was 245 at `41345ca`); asserted with `assert.Zero` + per-file line caps may only shrink | `internal/architecture/ratchet_test.go`; Phase 2 complete at `e3dd6fe` |
| ADR ratchet — `internal/agent/tools.go` | **0** raw SQL, **0** `env.Services` refs, 1,027 lines (was 1,186 lines / 1 SQL site / legacy service container) | same ratchet; Phase 3 complete at `a20aabf` |
| Legacy service container remaining live use | **231** `Services.*` call sites in `internal/handlers` across **21 fields**; container **30 fields** — Phase 4 increment 4 re-verified deletion blockers: `Experiments` (A/B admin handlers use legacy write/read API; 9 test refs), `FounderSignals` (write/ack path + audit wiring; 10 test refs), `PNL` (handlers + console money strip + nightly cron `GenerateDailySnapshot`; 13 test refs) — tests count as blockers, so nothing deleted yet | `grep` 2026-11-11 |
| Security gate | **Partial locally** — golangci-lint 0 issues across all branch changes; `govulncheck` + `-race` are CI-only on this android/arm64 host | `golangci-lint --new-from-rev=origin` 2026-11-10 |
| Migrations | **183 files each engine, head `00189_kharcha_read_permission.sql`**, next free `00190`, SQLite + PG mirrors | `git ls-files db/migrations*` 2026-11-17 |
| Schema | **181 tables / 285 indexes** | regenerated `db/schema.sql` 2026-11-17 (all 183 migrations, goose v189) |
| Internal packages | 90 top-level / 215 Go packages under `./internal/...` | `go list` 2026-11-17 |
| TODO/FIXME/HACK markers in `internal/` + `cmd/` | **0** | `grep -rn 'TODO\|FIXME\|HACK' --include='*.go'` |
| Packages without tests | 54 of 215 (`./internal/...`) | `go list -f` 2026-11-17 |
| OpenAPI | 206 hand-maintained paths (`openapi.yaml`); new driver payout routes not all spec'd | `grep -cE '^  /' openapi.yaml` |
| CI | 8 workflows; aggregate gate + backend build/test, security, mobile, PG parity, Playwright, deploy, restore drill, perf/mutation | `.github/workflows/` |
| PG rehearsal | **v185**, PG 18.2: 179/179 table parity, down→0→up clean, `-tags pg_integration` green, PG boot smokes 200; migrations `00186`–`00189` PG ports apply via CI `pg-parity` (PG 16, `goose up`) | `docs/tech-specs/c2-pg-cutover-runbook.md`; `.github/workflows/ci.yml` |

### Deltas since `544247ca` (2026-09-10 → 2026-11-10, refreshed 2026-11-11)
- **Architecture end-state executed (ADR Phases 2–3 complete)**: `internal/handlers/` raw SQL `245 → 0` at `e3dd6fe` (~40 handler files migrated into owning slice application packages; the CI ratchet now asserts zero rather than a cap), `internal/agent/tools.go` raw SQL `1 → 0` and legacy `env.Services` references `0` at `a20aabf` (20 tools now schema + argument mapping + delegate; 1,186 → 1,027 lines), and the pre-existing E-way-bill cross-tenant presentation/detail/write exposure was fixed at `504ddef` (reads scoped through `trips.tenant_id`, fail-closed, cross-tenant indistinguishable from unknown). Phase 4 cleanup inventory (2026-11-11): every legacy service still has live callers, so no component is safely deletable yet.
- **Trust surface truth pass**: `/status` is measured (availability sampler, real incident history, coverage-gated 30-day uptime — no 99.98% invention), `/pricing` 99.9% is a target, marketing copy de-fabricated; SLA breach alerting (00182) + auto-resolve on recovery; alert escalation defaults (00181) and **recipient targeting** (role → active tenant users, explicit recipients, cross-tenant excluded).
- **Money movement**: real **RazorpayX payout rail** (IMPS, penny-drop verification, signed webhook reconciliation, exactly-once wallet compensation), driver + admin payout UI, and a **stuck-payout sweep** (00184) that alerts when a payout sits in initiated/processing past threshold.
- **Privacy/retention**: business-data retention worker (00183 indexes; video 90d, audit/breach 3y, consents/DSAR 5y) and **driver KYC post-tenure purge** (00185: deactivated_at/kyc_purged_at, KYC fields + documents + local blobs purged after 3y, financial ledger preserved).
- **Reports/ops**: scheduled report delivery now real — email (SMTP pool/direct MX) and **webhook** (signed JSON + artifact, per-channel honest PENDING_DELIVERY/FAILED); backups are **AES-256 encrypted** with a decrypt-verifying monthly drill.
- **PG readiness**: two-phase migration helper + identity resync, `sqlite2pg --check` tenant-orphan gate, full PG 18.2 rehearsal at v185; the `00186`–`00189` PG ports are covered by the CI `pg-parity` job on PG 16 (`goose up`).
- **Mobile honesty (P0)**: removed fabricated Paisa balance/settlements, demo login credentials, fake nav/EWB/dispatch data, fake e-POD consignee defaults; added a source ratchet test.
- Multi-tenancy now **fail-closed by default** (`MULTI_TENANT_ENABLED=true`); legacy orphan-tenant users warn at boot with remediation SQL.
- **Audit hardening (PR #49, `1bc51d5` + review-pass `d8a112f`, 2026-11-17)**: migrations `00186`–`00189` split kharcha read/approve RBAC (admin + org_admin keep money authorization, dispatcher loses expense approval, drivers lose tenant-wide expense read), persist one-time password-reset/verify tokens (`00187`) and add a per-account login throttle (`00188`); Razorpay verify now binds order↔invoice server-side and records the captured amount; `/uploads/*` is served through a tenant-scoped `files` ACL; single-tenant mode fails closed on foreign-tenant users. Migration chain: 183 files/engine, head `00189`, next free `00190`.

## 2. What is genuinely strong

1. **Testing is real, not vanity.** 164 test-bearing Go packages (of 226; 0 FAIL per PR #49) with coverage floors enforced (61.2% total, 67.3% money paths); 68 mobile suites / 426 test cases in CI; ratchet tests pin typography, mock honesty, tenant isolation, locale parity, migration parity, mobile fake-data.
2. **Hygiene discipline.** 0 TODO/FIXME/HACK markers in 148.7k LOC; tenant lint at 0 warnings; security lint ratcheted to changed code.
3. **Migration governance.** 183-file append-only dual-engine history with ownership index and reserved-slot discipline (head `00189`, next free `00190`); v185 fully rehearsed on real PG 18.2 (up/down/re-up, 179/179 parity) with `00186`–`00189` PG ports covered by the PG-16 `pg-parity` job.
4. **Docs do not lie.** Roadmap/RELEASES/retention schedule self-report mock providers, pending creds, single-node limits; this survey supersedes stale claims (mock OCR ₹3000, thin OpenAPI, 99.98% status, 134 migrations).
5. **DDD islands done right.** `internal/booking/` and the new settlement payout seam (`internal/settlement/payout`, `monitor`) follow `docs/CONTRIBUTING.md` boundaries; domain does not import `net/http`/`database/sql`.
6. **Money safety is designed, not hoped.** Payout debit + instruction are one unit, provider failure/reversal compensates exactly once, webhook signature mandatory, stuck payouts escalate, KYC purge is idempotent, backups encrypted.

## 3. Honest weaknesses

### 3.1 Two architectures coexist (structural risk — now shrinking; ADR Phases 2–3 complete)
- **End-state executed, not just decided**: `docs/tech-specs/handlers-end-state-adr.md` set the boundary (handlers/ = HTTP+composition only; agent tools = schema + mapping only, delegate to application services) and the big-ticket migration is done: handlers/ raw SQL **245 → 0** at `e3dd6fe` (Phase 2, ~40 handler files moved into owning slice application packages; ratchet lowered from per-file caps to `assert.Zero`), and `internal/agent/tools.go` raw SQL **1 → 0**, legacy `env.Services` references **0**, lines **1,186 → 1,027** at `a20aabf` (Phase 3 complete across 20 tools: alerts, maintenance, money, booking/trip, dispatch/availability/dashboard/EWB facades). The E-way-bill cross-tenant presentation/detail/write exposure was closed at `504ddef` (tenant scoped via `trips.tenant_id`, fail-closed on empty tenant, unknown vs cross-tenant indistinguishable).
- **Remaining structural work (Phase 4)**: the legacy `service.Services` container still has **241 live `Services.*` call sites in handlers** (plus its own web/settlement/dashboard services), and the large god-files are capped-but-still-large (`trips.go` 1,917 cap, `app.go` 1,765, `maintenance.go` 1,081; caps lowered to the measured 2026-11-17 values). Nothing was deleted in the first Phase 4 increment because every legacy service still has live callers; deletion must follow call-site migration, not precede it. Score: architecture consistency **6 → 8/10** (evidence above), overall held at 8.5 because external acceptance remains the binding constraint.

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
- Go: 54 of 215 `./internal/...` packages have no test files (triage scaffolding vs real gaps); coverage depth uneven.
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
| Migrations & data governance | 9/10 | 183-file append-only dual-engine chain, head 00189 (next free 00190), ownership index, PG 18.2 v185 rehearsal (parity + down/up) + PG-16 pg-parity for 00186–00189 |
| Testing | 9/10 | 164 test-bearing Go packages (of 226; 0 FAIL per PR #49) + coverage floors (61.2% / money 67.3%), 68 mobile suites/426 test cases, ratchets; 54 no-test internal packages (of 215) and thin mobile branch margin remain |
| Architecture consistency | **8/10** (was 6) | end-state ADR executed: handlers/ raw SQL 245→0 with `assert.Zero` ratchet (`e3dd6fe`), agent/tools.go 0 SQL + 0 `env.Services` refs at 1,027 lines (`a20aabf`), EWB cross-tenant exposure fixed (`504ddef`), ~40 handler files migrated to slices; residual: 241 live `Services.*` handler call sites + large capped god-files (Phase 4) |
| Feature depth / prod readiness | 7/10 (was 6) | payout rail, retention (incl KYC), report delivery, encrypted backups, measured trust surface shipped; live provider creds, PG cutover, hardware, off-host restore proof still missing |
| Documentation honesty | 9/10 | roadmap/RELEASES/retention schedule self-report; this refresh replaces stale claims |
| **Overall** | **8.5/10** (held; architecture 6→8) | code-side depth + safety improved, architecture risk retired from the blocking list; external acceptance (creds, VPS, hardware) and Phase 4 legacy-service deletion still gate production |

*(Scores re-derived this survey from the evidence in §1; no re-grading of untouched areas without new evidence.)*

## 6. Recommended order of work

**External unblockers (acceptance, not code):** push auth (29 commits), RazorpayX creds (sandbox payout proof), SMS/OCR/FCM/email creds, R2 creds (off-host restore into staging), VPS freeze window (PG cutover), hardware/OEM decision.

**Then, in order:**
1. **PG live cutover** using the v185-rehearsed runbook (freeze → encrypted backup → `sqlite2pg --check` tenant-orphans=0 → live copy → PG smokes → flip → rollback kept).
2. **Provider go-live per `c4-provider-golive-runbook.md`** — EWB/GSTN/FASTag partner certs; implement Zoho/QuickBooks live adapters (currently mock-only).
3. **Ops acceptance**: verify stuck-payout escalation + alert recipient targeting in a real tenant; reconcile KYC blob `blobs_pending` manual queue.
4. **Phase 4 cleanup**: migrate the remaining **241** `Services.*` handler call sites onto slice facades, then delete the replaced legacy services and `App.DB` fields domain by domain (first inventory at 2026-11-11 found no whole component safely deletable yet); keep the ratchet caps shrinking.
5. **OpenAPI spec** for the new driver payout/webhook routes; re-run A6 parity audit.
6. **Test-debt triage**: 54 no-test internal packages (of 215); mobile branch margin; keep race/govulncheck/soak in CI.

## 7. Sources

- `/data/data/com.termux/files/home/dsh-verify/final-suite.log` — `go test -shuffle ./internal/... ./test/...`, 2026-11-10 (148 ok / 0 FAIL at `41345ca`); counts refreshed 2026-11-17 at `1bc51d5`: `go list ./...` = 226 packages (164 carry tests), `git ls-files db/migrations*` = 183/183, regenerated `db/schema.sql` = 181 tables / 285 indexes
- `/data/data/com.termux/files/home/dsh-verify/mobile-coverage.log` — `jest --coverage`, 2026-11-10
- `go tool cover -func=coverage.out`; `scripts/check-coverage.sh` floors; `golangci-lint --new-from-rev=origin`
- `docs/tech-specs/c2-pg-cutover-runbook.md` (PG 18.2 v185 rehearsal); `docs/RELEASES.md`; `docs/ROADMAP.md`; `docs/PLAN-fleetx-parity-2026-10-28.md`
- `internal/architecture/ratchet_test.go` + `docs/tech-specs/handlers-end-state-adr.md` — end-state evidence at `e3dd6fe` (handlers zero SQL), `a20aabf` (agent tools zero SQL / no legacy service access), `504ddef` (EWB tenant-scoping fix); Phase 4 inventory `grep` run 2026-11-11
- Counts refreshed 2026-11-17 on `1bc51d5` (migrations/schema/packages); metric baselines measured on commit `41345ca`; prior surveys `544247ca` (2026-09-10), `3f015a8b` (2026-09-08), `ea5b078`
