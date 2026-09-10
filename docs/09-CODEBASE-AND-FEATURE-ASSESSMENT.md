# 09 — Codebase & Feature Assessment

> Evidence-based snapshot of built state, quality, and honest gaps. Survey date: **2026-09-10 (refresh)**, commit `544247ca` (master, clean tree). Supersedes the `3f015a8b` (2026-09-08) and `ea5b078` surveys in this file; deltas called out per section. `docs/*.md` supersede on conflict (per `docs/ROADMAP.md` header rule).

---

## 1. Measured metrics

| Metric | Value | Source |
|---|---|---|
| Non-test Go LOC (`internal/` + `cmd/`) | 109.7k / 547 Go files | `find internal cmd -name '*.go' ! -name '*_test.go' \| xargs wc -l` |
| Test LOC / test files (Go) | 69.2k / 343 `_test.go` files (~63% test:code ratio) | same method |
| Test result (Go) | 120 packages `ok`, **0 FAIL** | `go test ./internal/... ./db/... -count=1` 2026-09-10, `/tmp/gotest_refresh.log` |
| Mobile tests | 56 suites / 351 tests, all pass (re-verified 2026-09-10); coverage NOT re-measured this refresh — last 85.5% stmt / 76.2% branch / 84.1% func / 88.1% line | `npx jest` 2026-09-10; `npx jest --coverage` 2026-09-08 |
| Build / vet / gofmt | exit 0 / exit 0 (2026-09-10); gofmt last clean 2026-09-08 (not re-run) | `go build ./...` + `go vet ./...` 2026-09-10 |
| Security gate | Pass (tenant literals, SQL tenant check, tenant lint, secrets) | `LINT_BASE=$(git rev-parse HEAD) ./scripts/security-check.sh` — re-run 2026-09-10: all checks passed |
| Migrations | 134 files each engine, head `00140`, SQLite + PG mirrors | `ls db/migrations*/ *.sql`; 6 migrations since `3f015a8b` (`00135`–`00140`) |
| Internal packages | 59 top-level / 170 with code | directory listing |
| TODO/FIXME/HACK markers in `internal/` | **4** (re-verified 2026-09-10) | `grep -rn 'TODO\|FIXME\|HACK' --include='*.go' internal/` |
| Packages without tests | 52 `no test files` rows (scope includes `./db/...`) | `/tmp/gotest_refresh.log` |
| CI state | `CI` green as of 2026-09-08 (not re-checked this refresh); `Mobile CI` per-push Stryker removed | gh runs `34258897776` success; commits `5ffb5429`/`b2c9bd24`/`07cc3742`/`3f015a8b` |

### Deltas since `ea5b078`
- `00129`–`00134`: GSTN/verify seam (`00130`), license NULL backfill (`00131`), vehicle expiry nullable (`00132`), `email_verified_at` (`00133`), snapshot `ts_unix` (`00134`).
- Tracking hardening stack (`b83d328f`, +2740/−3160): Go-side visibility window, `ts_unix` canonical clock, SSE tenant isolation + fracture fix, MQTT/GT06/AIS-140 edge tests, storage schema upgrade path, `offlineQueue` id-based reconcile.
- Mobile dead-code purge: `FirstTimeSetupScreen` (1077 lines, fake DL + 2032 expiry), phantom telemetry pipeline, vault/esign/dataRights/fastag/ewaybill stubs.
- Mobile coverage now measured and gated (was "unquantified" §3.4): 75% branch threshold enforced, currently 76.2%.
- CI: `backend-test` timeout 15→30 min (suite outgrew budget); PG parity test derives version from files (was hardcoded 128); per-push Stryker dropped (nightly covers it).

### Deltas since `3f015a8b` (2026-09-09 → 10, 28 commits)
- Migrations `00135`–`00140`: per-tenant vehicle plates + latest-position tenant uniqueness; `telemetry_positions` NULL backfill; `vehicle_commands` + `av_operator`; outbox/alerts/fuel partial scale indexes; sessions/KPI/audit scale indexes; `files.tenant_id` (closes cross-tenant file-read hole).
- Telemetry round 2: ack-after-accept + worker failure metrics; etag-304/envelope metadata/spatial-bbox/delta sync on live endpoint; SSE keep-alive; sync tenant-guard closes cross-tenant spoof via device/vehicle/driver IDs; partition retention cleaner.
- Infra/ops: S3/R2 storage driver; Cloudflare Turnstile bot protection; egress origin shield + immutable static caching; cross-compiled VPS deploy (`scripts/deploy-vps.sh`, systemd, rsync) with compile-on-target blocked.
- Product: AV teleoperation deck + fleet route optimizer; PWA service worker + offline precache shell + client image compression; Google gl=IN live-map tiles + India viewport bounds; trips auto-seed default pickup/drop stops wired to `/trips/new`.
- A6/A8 closed with evidence (`openapi-router-parity-a6.md`, `legacy-delegation-adr.md`); SOP trip-close odometer shipped (B1 seed).

## 2. What is genuinely strong

1. **Testing is real, not vanity.** 337 Go test files + 56 mobile suites, all green 2026-09-08. Coverage now quantified on mobile; Go coverage gate (`scripts/check-coverage.sh`, 80%) enforced in CI.
2. **Hygiene discipline.** 4 TODO markers in 105k LOC. Tenant lint at 0 warnings. Security gate green and ratcheted to `LINT_BASE`.
3. **Migration governance.** 128-file append-only history with ownership index, reserved-slot discipline, dual-engine parity with CI gate — plus the parity test can no longer rot (derives head version from files).
4. **Docs do not lie.** `docs/ROADMAP.md` self-reports stale sections, mock-only providers, FK violations, single-node fragility. `docs/RELEASES.md` now records the tracking-hardening release.
5. **DDD islands done right.** `internal/booking/` follows `docs/CONTRIBUTING.md` boundaries; domain does not import `net/http`/`database/sql`.
6. **RAG now holds operational knowledge.** Telemetry state machine, GPS→map pipeline, event-bus naming/SSE isolation, `ts_unix` seam taught and verified retrievable.

## 3. Honest weaknesses

### 3.1 Two architectures coexist (highest structural risk — unchanged)
- `internal/handlers/` god-files persist: `trips.go` (1,818), `app.go` (1,482), `invoices.go` (1,262), `invoices_consolidate.go` (1,065). New entry: `internal/agent/tools.go` (1,153) — agent tool growth is recreating the pattern in a new directory.
- **No written end-state for the handlers/ god-files.** Roadmap A8 covers vehicle only.

### 3.2 Debt hides in docs, not code (unchanged, one item closed)
- FK violations (A3, open), stale architecture-doc sections (C5, open). Closed this cycle: nothing from the Phase A list — the cycle's work was telemetry/CI, not roadmap debt. The warning from the last survey stands: fine only while Phase A is actually worked.

### 3.3 Production readiness gaps (feature depth)
| Area | State | Proof |
|---|---|---|
| Money/compliance integrations (EWB, GSTN, FASTag, accounting) | **mock-by-default** | roadmap C4; mock-honesty flags |
| Mobile driver app | 16 screens: `src/screens/*.ts` are re-export barrels for `src/components/*Screen.tsx` (all full React Native components: ActiveNav 25KB, DeliveryVerify 32KB, Profile 31KB, Expense 17KB, Paisa 16KB, etc.); verified wired to mobile services | roadmap A10 (verified); `mobile/README.md` |
| OpenAPI coverage | ~1 domain (`/api/v1/vehicles`) out of dozens of mounted APIs | roadmap A6/B12 (open) |
| Scale | single Android device VPS; no scale promises before PG cutover | FAILURE_ANALYSIS; roadmap C1/C2 |
| Data integrity | FK violations disposition pending | roadmap A3 (open) |
| AI assistant safety | approval gate exists, RL loop local-first — depends on flag hygiene in prod | AGENTS.md guardrails |
| Telemetry clock seam (new) | `timestamp` TEXT holds unparseable Go-String layouts; `ts_unix` is canonical — any new reader using `datetime(timestamp)` silently drops rows | `00134`, docs/02 |

### 3.4 Test coverage is broad; mobile now quantified, Go depth still uneven
- Mobile: 76.2% branches (threshold 75%) — thin margin; next uncovered-branch addition re-breaks the gate.
- Go: pass/fail proven + 80% gate in CI, but per-package depth uneven (`driver/domain/aggregate` untested vs `driver/domain/eligibility` tested). 53 `no test files` packages under the widened `./db/...` scope need triage to separate scaffolding from real gaps.

## 4. Feature inventory (built, verified via migrations per roadmap)

- **Core ops:** booking → trip state machine (dwell/detention) → geofence → dispatch offers/quotes → E-way bills incl. multi-leg + delivered lifecycle (`00112/00113/00127/00128`)
- **Billing:** GST engine, PDF/QR invoices, credit notes, settlement ledger + payouts (`00111`), Razorpay
- **Telemetry (hardened, two cycles):** GT06/AIS-140 TCP `:5023` + MQTT `:1883` ingest, provider parity (`00117`), route/ETA jobs (`00066/00067`); Go-side visibility window, `ts_unix` clock (`00134`), SSE tenant isolation, quarantine + sync reconcile semantics; round 2 (`00135`/`00136`): per-tenant plates, NULL backfill, delta/etag live sync, ack-after-accept, retention cleaner, sync-spoof fix
- **Fleet:** vehicle compliance hard-block, work orders (`00123`), maintenance annual estimates (`00124`), fuel engine + audits, fleet SOP parity JSON + SAP-tab HTML (`00126`); expiry fields nullable (`00132`)
- **People:** driver lifecycle (`00108`), license NULL semantics (`00131`), kharcha, scorecards
- **Platform:** multi-tenancy registry + trigger hardening (`00102–00105`), entitlements/subscriptions/webhooks (`00114/00115`), company profiles (`00125`), comm outbox + email pool (`00118/00120`), leader leases (`00079`), RAG with RBAC (`00078`), agent orchestrator + RL + approval gate; S3/R2 storage driver + `files.tenant_id` (`00140`), AV teleoperation deck + `vehicle_commands` (`00137`), PWA service worker, Turnstile bot protection, scale indexes (`00138/00139`)
- **Onboarding (completed this cycle):** GSTN checksum gate, fail-hard trial seed, email-verify seam (`00130/00133`), funnel API, wizard draft, reviewer gating
- **Not built (zero code, 2026-09-10 grep):** ZMOTM SOP *report set* (`ZMOTM_MR`, `IP41`, `IW28` — the `ZMOTM_MMS` p.8 trip-close odometer shipped as B1 seed), backhaul matching (`00068` reserved), STO portal (`00069`), CX timeline (`00070`), fuel cards (`00071`), ESG (`00072`) — Phase B

## 5. Scorecard

| Dimension | Score | Basis |
|---|---|---|
| Security & tenancy | 9/10 | gate green, SSE tenant isolation landed; A1 secret rotation still open |
| Migrations & data governance | 9/10 | append-only, dual-engine, ownership index (head `00140`), self-deriving parity test |
| Testing | 9/10 (was 8) | mobile coverage quantified + gated, full suite 120 ok / 0 FAIL; Go per-package unevenness remains (52 no-test packages) |
| Architecture consistency | 6/10 | god-files unchanged; `agent/tools.go` (1,153) is a new instance of the pattern; no end-state ADR |
| Feature depth / prod readiness | 6/10 | mock providers, stub mobile, thin OpenAPI; onboarding + telemetry depth improved |
| Documentation honesty | 9/10 | self-reporting roadmap; this refresh closes the `ea5b078` staleness |
| **Overall** | **8/10** (was 7.5) | telemetry/onboarding depth + CI/test quantification; product surface still ~6 |

*(Scores carried from the 2026-09-08 survey. The 2026-09-10 refresh found 28 commits with all gates green — build, vet, 120/0 FAIL tests, mobile 56/56, security gate pass — no regression signals; no re-grading claimed without new evidence.)*

## 6. Recommended order of work (endorsed, matches roadmap)

1. **A3** FK-health triage (61 violations) — cheapest integrity win, untouched two surveys running
2. **A8** Legacy-delegation ADR — now covers `handlers/` god-files **and** `agent/tools.go`; decide end-state before more growth
3. **A6** OpenAPI↔router parity audit — makes B12 gap closure measurable
4. **A10** Mobile stub inventory — closed with verified inventory (`src/screens/*.ts` barrels -> `src/components/*Screen.tsx` implementations)
5. Then Phase B in migration-slot order (`00142+`; head is `00141`)

## 7. Sources

- `/tmp/gotest_refresh.log` (`go test ./internal/... ./db/... -count=1`, 2026-09-10 refresh)
- `npx jest` (mobile, 2026-09-10 re-verify; coverage last measured 2026-09-08)
- `./scripts/security-check.sh` (2026-09-10 re-run)
- `docs/ROADMAP.md`, `docs/RELEASES.md`, `FAILURE_ANALYSIS_2026-08-31.md`, `docs/CONTRIBUTING.md`
- Measured on commit `544247ca`; prior surveys `3f015a8b` (2026-09-08), `ea5b078`
