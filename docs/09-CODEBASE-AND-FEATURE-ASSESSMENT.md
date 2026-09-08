# 09 — Codebase & Feature Assessment

> Evidence-based snapshot of built state, quality, and honest gaps. Survey date: **2026-09-08 (refresh)**, commit `3f015a8b` (master, clean tree). Supersedes the `ea5b078` survey in this file; deltas called out per section. `docs/*.md` supersede on conflict (per `docs/ROADMAP.md` header rule).

---

## 1. Measured metrics

| Metric | Value | Source |
|---|---|---|
| Non-test Go LOC (`internal/` + `cmd/`) | 105k / 544 Go files | `find internal cmd -name '*.go' ! -name '*_test.go' \| xargs wc -l` |
| Test LOC / test files (Go) | 67.5k / 337 `_test.go` files (~64% test:code ratio) | same method |
| Test result (Go) | 119 packages `ok`, **0 FAIL** | `go test ./internal/... ./db/... -count=1` 2026-09-08, `/tmp/gotest_0908.log` |
| Mobile tests | 56 suites / 351 tests, all pass; coverage 85.5% stmt / 76.2% branch / 84.1% func / 88.1% line | `npx jest --coverage` 2026-09-08 |
| Build / vet / gofmt | exit 0 / exit 0 / clean | 2026-09-08 refresh run |
| Security gate | Pass (tenant literals, SQL tenant check, tenant lint, secrets) | `LINT_BASE=$(git rev-parse HEAD) ./scripts/security-check.sh` |
| Migrations | 128 files each engine, head `00134`, SQLite + PG mirrors | `ls db/migrations*/ *.sql`; 6 migrations since `ea5b078` (`00129`–`00134`) |
| Internal packages | 59 top-level / 170 with code | directory listing |
| TODO/FIXME/HACK markers in `internal/` | **4** (unchanged) | `grep -rn 'TODO\|FIXME\|HACK' --include='*.go' internal/` |
| Packages without tests | 53 `no test files` rows (was 19 — scope now includes `./db/...`) | `/tmp/gotest_0908.log` |
| CI state | `CI` green after 2 red→green fixes this cycle (PG parity, mobile coverage); `Mobile CI` per-push Stryker removed | gh runs `34258897776` success; commits `5ffb5429`/`b2c9bd24`/`07cc3742`/`3f015a8b` |

### Deltas since `ea5b078`
- `00129`–`00134`: GSTN/verify seam (`00130`), license NULL backfill (`00131`), vehicle expiry nullable (`00132`), `email_verified_at` (`00133`), snapshot `ts_unix` (`00134`).
- Tracking hardening stack (`b83d328f`, +2740/−3160): Go-side visibility window, `ts_unix` canonical clock, SSE tenant isolation + fracture fix, MQTT/GT06/AIS-140 edge tests, storage schema upgrade path, `offlineQueue` id-based reconcile.
- Mobile dead-code purge: `FirstTimeSetupScreen` (1077 lines, fake DL + 2032 expiry), phantom telemetry pipeline, vault/esign/dataRights/fastag/ewaybill stubs.
- Mobile coverage now measured and gated (was "unquantified" §3.4): 75% branch threshold enforced, currently 76.2%.
- CI: `backend-test` timeout 15→30 min (suite outgrew budget); PG parity test derives version from files (was hardcoded 128); per-push Stryker dropped (nightly covers it).

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
| Mobile driver app | 16 screens, only `DispatchScreen`/`TripsScreen` are real `.tsx`; rest `.ts` stubs — one stub (`FirstTimeSetupScreen`) deleted this cycle, inventory otherwise unverified | roadmap A10 (open); `ls mobile/src/screens/` |
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
- **Telemetry (hardened this cycle):** GT06/AIS-140 TCP `:5023` + MQTT `:1883` ingest, provider parity (`00117`), route/ETA jobs (`00066/00067`); Go-side visibility window, `ts_unix` clock (`00134`), SSE tenant isolation, quarantine + sync reconcile semantics
- **Fleet:** vehicle compliance hard-block, work orders (`00123`), maintenance annual estimates (`00124`), fuel engine + audits, fleet SOP parity JSON + SAP-tab HTML (`00126`); expiry fields nullable (`00132`)
- **People:** driver lifecycle (`00108`), license NULL semantics (`00131`), kharcha, scorecards
- **Platform:** multi-tenancy registry + trigger hardening (`00102–00105`), entitlements/subscriptions/webhooks (`00114/00115`), company profiles (`00125`), comm outbox + email pool (`00118/00120`), leader leases (`00079`), RAG with RBAC (`00078`), agent orchestrator + RL + approval gate
- **Onboarding (completed this cycle):** GSTN checksum gate, fail-hard trial seed, email-verify seam (`00130/00133`), funnel API, wizard draft, reviewer gating
- **Not built (zero code):** ZMOTM SOP reports, backhaul matching (`00068` reserved), STO portal (`00069`), CX timeline (`00070`), fuel cards (`00071`), ESG (`00072`) — all scheduled Phase B

## 5. Scorecard

| Dimension | Score | Basis |
|---|---|---|
| Security & tenancy | 9/10 | gate green, SSE tenant isolation landed; A1 secret rotation still open |
| Migrations & data governance | 9/10 | append-only, dual-engine, ownership index, self-deriving parity test |
| Testing | 9/10 (was 8) | mobile coverage quantified + gated, full suite 119 ok / 0 FAIL; Go per-package unevenness remains |
| Architecture consistency | 6/10 | god-files unchanged; `agent/tools.go` (1,153) is a new instance of the pattern; no end-state ADR |
| Feature depth / prod readiness | 6/10 | mock providers, stub mobile, thin OpenAPI; onboarding + telemetry depth improved |
| Documentation honesty | 9/10 | self-reporting roadmap; this refresh closes the `ea5b078` staleness |
| **Overall** | **8/10** (was 7.5) | telemetry/onboarding depth + CI/test quantification; product surface still ~6 |

## 6. Recommended order of work (endorsed, matches roadmap)

1. **A3** FK-health triage (61 violations) — cheapest integrity win, untouched two surveys running
2. **A8** Legacy-delegation ADR — now covers `handlers/` god-files **and** `agent/tools.go`; decide end-state before more growth
3. **A6** OpenAPI↔router parity audit — makes B12 gap closure measurable
4. **A10** Mobile stub inventory — one stub deleted this cycle; 14 `.ts` stubs remain unverified
5. Then Phase B in migration-slot order (`00135+`; head is `00134`)

## 7. Sources

- `/tmp/gotest_0908.log` (`go test ./internal/... ./db/... -count=1`, 2026-09-08 refresh)
- `npx jest --coverage` + `npx tsc --noEmit` (mobile, 2026-09-08 refresh)
- `./scripts/security-check.sh` (2026-09-08 refresh)
- `docs/ROADMAP.md`, `docs/RELEASES.md`, `FAILURE_ANALYSIS_2026-08-31.md`, `docs/CONTRIBUTING.md`
- Measured on commit `3f015a8b`; prior survey commit `ea5b078`
