# ADR: End-state architecture for `internal/handlers/` and `internal/agent/tools.go`

Date: 2026-11-10. Status: accepted. Supersedes the "coexist indefinitely" growth
policy in `docs/tech-specs/legacy-delegation-adr.md` (its freeze +
opportunistic-delegation rules stay in force; this ADR adds the explicit target
boundary, migration order, and measurable exit criteria). Closes the
"no written end-state" finding in `docs/09` §3.1.

## Context

Measured baseline at head `41345ca` (2026-11-10):

| File | Lines | Raw SQL call sites |
|---|---:|---:|
| `internal/handlers/trips.go` | 2,035 | 20 |
| `internal/handlers/app.go` | 1,765 | 0 (composition only) |
| `internal/handlers/invoices_consolidate.go` | 1,674 | 14 |
| `internal/handlers/sto.go` | 1,287 | 0 |
| `internal/handlers/invoices.go` | 1,259 | 21 |
| `internal/handlers/zmotm_reports.go` | 961 (was 1,166) | 7 (was 9) |
| `internal/handlers/reports.go` | 1,146 | 0 |
| `internal/handlers/maintenance.go` | 1,103 | 3 |
| `internal/handlers/auth.go` | 1,011 | 0 |
| `internal/handlers/share.go` | 999 | 16 |
| `internal/handlers/vehicles.go` | 950 | 14 |
| `internal/handlers/maintenance_plans.go` | 910 | 2 |
| `internal/handlers/drivers.go` | 812 | 22 |
| `internal/handlers/customer_portal.go` | 811 | 16 |
| `internal/agent/tools.go` | 1,186 | 1 |
| **handlers/ package total** | — | **243** (was 245) |

Why this matters:
- The DDD boundaries in `docs/CONTRIBUTING.md` are real and proven by
  vertical slices (`internal/booking`, `internal/settlement`, `internal/driver`,
  `internal/privacy`, `internal/alerts`, `internal/reportbuilder`,
  `internal/retention`). `internal/handlers/` still mixes HTTP,
  business rules, and raw SQL; `internal/agent/tools.go` repeats the
  pattern for AI tools.
- `legacy-delegation-adr.md` froze growth and prescribed opportunistic
  extraction, but explicitly allowed the two architectures to coexist
  indefinitely. `docs/09` scores architecture consistency 6/10 precisely
  because no end-state or exit criteria existed.
- A big-bang rewrite is unsafe: these paths serve production traffic and are
  pinned by e2e/characterization tests, not by unit tests of the god-files.

## Decision

### 1. Target boundary (the end-state)

`internal/handlers/` is the **HTTP + composition layer only**:
1. parse/validate request shape,
2. authorize (existing middleware/permission helpers),
3. call one application use-case / facade,
4. map result to JSON or a template view.

It may contain **zero raw SQL** and **zero business invariants** (no status
transitions, no amount/GST/commission math, no retry/idempotency rules).

`internal/agent/tools.go` is the **tool schema + argument mapping layer
only**: each tool delegates to the owning application service. Zero SQL, zero
business rules.

### 2. Canonical slice layout (per domain)

`
internal/<domain>/
  domain/                     entities, invariants, repository interfaces
  application/                use-cases, orchestration, transaction boundaries
  infrastructure/persistence/sql/   repository implementations
  presentation/api/handlers/  JSON API handlers (thin)
`

Web HTML pages stay in `internal/handlers/<feature>_web.go` as **view
adapters** over the same application service (parse form → call UC → render
template). They may exceed 400 lines only while pure rendering; no SQL and no
business branches.

Shared kernel: `handlers.App` remains the composition root (config,
templates, auth, middleware, DB handle). Once a domain migrates, it gets its own
service wired into `App`; `App.DB` must not be used by migrated domains.

### 3. What stays legacy permanently

- Web page rendering and form parsing in `internal/handlers/` (allowed to
  remain, but must delegate).
- `internal/service/` legacy services for domains not yet sliced; frozen,
  no new features.
- `App.DB` access for not-yet-migrated code only, ratcheted downward by the
  architecture test (see §5).

### 4. Migration order (one domain per PR, behavior frozen)

**Phase 0 — ratchet + guardrails (this ADR).**
- `internal/architecture/ratchet_test.go` caps handler SQL sites at the
  measured baseline (243 total, per-file caps), `agent/tools.go` SQL at 1,
  and god-file line counts at current sizes. Caps may only be **lowered**.
- Review rule: no new business logic or SQL in `handlers/` or
  `agent/tools.go`; new behavior goes to a slice UC first.

**Phase 1 progress (2026-11-10):** vehicle-master + gate-register query paths moved to `internal/fleetreports` (`Service.VehicleMaster`/`GateRegister`); `zmotm_reports.go` is now 961 lines / 7 SQL sites (was 1,166 / 9) and the ratchet caps were lowered in the same PR. Characterization tests (zmotm_reports_test.go) pin the JSON/CSV behavior.

**Phase 1 — read/query paths** (lowest risk): `reports.go`,
`zmotm_reports.go`, `share.go`, `customer_portal.go`,
`kpis.go`, `console.go` → read-model queries in their domain's
`application` package.

**Phase 2 — money + operations write paths** (characterization tests first):
`trips.go` (trip lifecycle already has a domain slice),
`invoices.go` + `invoices_consolidate.go` (GST/PDF/credit notes),
`vehicles.go`, `drivers.go`, `maintenance*.go`.

**Phase 3 — agent tools**: each `tools.go` tool maps to an application
facade; remove inline SQL; approval gate stays where it is.

**Phase 4 — cleanup**: delete replaced legacy services and `App.DB`
fields domain by domain; update `docs/09` architecture score.

### 5. Exit criteria per file/domain

- zero raw SQL in the handler/tool file (ratchet cap 0),
- no business invariant branches (reviewed; status/amount logic lives in domain),
- handler/tool ≤400 lines (web view adapters ≤800, rendering-only),
- characterization tests added **before** extraction (they pin behavior and
  survive the move),
- `App.DB` not referenced by the migrated domain,
- ratchet caps lowered in the same PR that shrinks the file.

## Alternatives considered

1. **Big-bang rewrite** — rejected: production behavior is pinned by e2e tests,
   not unit tests; high regression risk, no incremental value.
2. **Keep the two architectures indefinitely** (previous ADR) — rejected: the
   measured growth (trips 2,035; agent tools 1,186) shows the pattern recreates
   itself; docs/09 architecture score stays at 6/10 with no exit path.
3. **Framework/router switch** — orthogonal; does not address domain boundaries.
4. **Freeze-only, no ratchet** — rejected: unenforced freezes drift; a failing
   test is the only reliable boundary.

## Consequences

- Two architectures coexist during the transition, but the direction is now
  one-way and measurable: SQL counts and line caps can only go down.
- Every extraction PR must lower the relevant ratchet cap, so progress is
  visible in CI rather than in a planning document.
- Some legacy web pages remain in `handlers/` permanently by design; the
  ADR distinguishes "view adapter" from "business handler" so that is not a
  failure.
- The ratchet baseline is intentionally generous today; it exists to stop
  growth, not to claim the code is clean.

## References

- `docs/CONTRIBUTING.md` (DDD boundaries), `docs/tech-specs/legacy-delegation-adr.md`
- `docs/09-CODEBASE-AND-FEATURE-ASSESSMENT.md` §3.1, §5 (architecture 6/10)
- `docs/ROADMAP.md` Phase A8
- `internal/architecture/ratchet_test.go` (enforcement)
