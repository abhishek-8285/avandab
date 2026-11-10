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
| `internal/handlers/zmotm_reports.go` | 476 (was 1,166) | 0 (was 9) |
| `internal/handlers/reports.go` | 1,146 | 0 |
| `internal/handlers/maintenance.go` | 1,103 | 3 |
| `internal/handlers/auth.go` | 1,011 | 0 |
| `internal/handlers/share.go` | 999 | 16 |
| `internal/handlers/vehicles.go` | 950 | 14 |
| `internal/handlers/maintenance_plans.go` | 910 | 2 |
| `internal/handlers/drivers.go` | 812 | 22 |
| `internal/handlers/customer_portal.go` | 811 | 16 |
| `internal/agent/tools.go` | 1,186 | 1 |
| **handlers/ package total** | — | **197** (was 245) |

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
  measured baseline (197 total, per-file caps), `agent/tools.go` SQL at 1,
  and god-file line counts at current sizes. Caps may only be **lowered**.
- Review rule: no new business logic or SQL in `handlers/` or
  `agent/tools.go`; new behavior goes to a slice UC first.

**Phase 1 progress (2026-11-10, fifth domain - Phase 1 read paths complete):** the Command Center read paths (fleet strip, vehicle context panel) moved to `internal/controltower/application` (`ConsoleReadModel.Fleet`, `VehicleContext`); `internal/handlers/console.go` is now 362 lines / 5 SQL sites (was 591 / 15 - remaining are the inline e-way-bill extend action + audit write, Phase 2), handlers/ total 197 (was 245 at baseline, -48 SQL sites in Phase 1), caps lowered in the same PR. Console render tests + controltower application suites pass.

**Phase 1 progress (2026-11-10, fourth domain):** the shipper-portal read paths moved to `internal/customerportal` (`Bookings`, `Invoices`, `Tracking`, `Stops`, all scoped via customer_users); `internal/handlers/customer_portal.go` is now 588 lines / 9 SQL sites (was 811 / 16 - remaining are feedback writes/validation reads, last-seen helper, brand lookup), handlers/ total 207, caps lowered in the same PR. Portal scoping tests (TestPortalInvoices_ScopedToOwnCustomer, TestLastSeenFix_*) pin behavior.

**Phase 1 progress (2026-11-10, third domain):** the public share/playback read paths moved to `internal/sharing` (`ByToken`, `PINByToken`, `DataByToken`, `Snapshot`, `MaintenanceDue`, `Polyline`, `TimelineByToken`, `List`); `internal/handlers/share.go` is now 870 lines / 8 SQL sites (was 999 / 16 - the 8 remaining are the create/view-count/revoke writes and create-path reads, Phase 2), handlers/ total 214, caps lowered in the same PR. Existing share characterization tests (share_test.go: create/view/PIN/data/list/revoke/timeline) pin behavior.

**Phase 1 progress (2026-11-10, second domain):** the dashboard KPI read paths moved to `internal/kpi` (`Service.Bookings`, `Trips`, `Vehicles`, `Drivers`, `Payments`, `Invoices`); `internal/handlers/kpis.go` is now 41 lines / 0 SQL sites (was 239 / 14), handlers/ total 222 (was 236), caps lowered in the same PR. Characterization tests exposed a real formatting bug (`trimFloat` cut trailing zeros from integer rupees, e.g. ₹300 rendered as ₹3), fixed and pinned. 

**Phase 1 progress (2026-11-10):** the whole ZMOTM_MR read-path domain moved to `internal/fleetreports` (`Service.VehicleMaster`, `GateRegister`, `FuelKMPL`, `Breakdown`, `KMPLSummary`); `zmotm_reports.go` is now 476 lines / 0 SQL sites (was 1,166 / 9), handlers/ total 236 (was 245), and the ratchet caps were lowered in the same PR. Characterization tests (zmotm_reports_test.go, zmotm_kmpl_summary_test.go) pin the JSON/CSV behavior.

**Phase 1 — read/query paths** (lowest risk): `reports.go`,
`zmotm_reports.go`, `share.go`, `customer_portal.go`,
`kpis.go`, `console.go` → read-model queries in their domain's
`application` package.

**Phase 2 — money + operations write paths** (characterization tests first):
`trips.go` (trip lifecycle already has a domain slice),
`invoices.go` + `invoices_consolidate.go` (GST/PDF/credit notes),
`vehicles.go`, `drivers.go`, `maintenance*.go`.

**Phase 2 progress (2026-11-10, first write increment - shares):** the
share-link write paths moved to `internal/sharing` (`Create` - trip-tenant
check + per-trip active cap + insert, `RecordView` - sliding expiry + view
counter, `RecordPINFailure`, `RecordPINSuccess`, `Revoke`, `IsDriverOnTrip`),
with sentinel errors (`ErrTripNotFound`, `ErrMaxActive`, `ErrLinkCreate`)
preserving the handler's 404/409/500 statuses and messages;
`internal/handlers/share.go` is now 826 lines / 0 SQL sites (was 870 / 8),
handlers/ total 189 (was 197), caps lowered in the same PR. Existing share
characterization tests (share_test.go: create/view/PIN/data/list/revoke/
timeline) pass unchanged; new sharing write tests pin tenant scoping, the
active cap and sentinel behavior. Note: the create-path failure precedence is
unchanged for trip/cap/insert, but request parsing now runs before the
tenant/cap checks (needed to bundle the checks with the INSERT in one
use-case call); no status/JSON shape changed for valid requests.

**Phase 2 progress (2026-11-10, second write increment - trips):** the trip
write side-effect paths moved to `internal/trip/application`
(`RecordDispatchOverrideUseCase` - `dispatch_overrides` row plus the audit
service/raw-audit fallback with the legacy audit precedence;
`BlockVehicleForBreakdownUseCase` - trip-close breakdown vehicle
maintenance/blocked flag plus the shared operator-facing description;
`MirrorPODArtifactsUseCase` - legacy `trips` POD columns and
`stop_pod_attachments` mirror), preserving tenant scoping, the exact
descriptions/statuses, the audit write and the legacy best-effort error
semantics (errors are returned for logging, never surfaced as HTTP failures).
The ops-breakdown alert stays handler-filed because `CompleteTripUseCase`
explicitly leaves breakdown alerts to callers. `internal/handlers/trips.go` is
now 2,010 lines / 12 SQL sites (was 2,035 / 20; the remaining 12 are reads:
trip view, e-POD certificate, compliance prechecks, preferred-vehicle lookups,
ePOD stop auto-locate), handlers/ total 181 (was 189), caps lowered in the
same PR. Existing trip handler tests (trips_selected_test.go,
trips_complete_detention_test.go, trip_create_assign_flash_test.go) pass
unchanged; new `internal/trip/application/trip_write_use_cases_test.go` pins
tenant isolation, vehicle resolution, the exact breakdown description and the
legacy POD mirror behaviour.

**Phase 2 progress (2026-11-10, third increment - trip read paths):** the
remaining trip read queries moved to `internal/trip/application`
(`TripReadQueries` read model: `PODConsigneePhone` - POD OTP SMS consignee
phone; `DriverPreferredVehicles` - new/edit driver→vehicle prefill map;
`TripStops` - trip view stop list; `AssignmentRefs` - dispatch-compliance
prechecks; `LastStop` - ePOD last-stop auto-locate; `PublicEPODCertificate` -
trip/vehicle/driver/tenant/trip_stops data for the login-free certificate),
with sentinel `ErrEPODCertificateNotFound` preserving the handler's 404 and
the legacy best-effort companion lookups (vehicle/driver/tenant/stops errors
leave blank fields). Tenant scoping is unchanged: the tenant-scoped queries
still receive the trip's tenant (`shared.TenantIDFromContext`/`MustTenantID`
at the call sites), while the public certificate keeps its legacy
id-or-trip-number lookup and derives branding from the trip's own tenant row
(never the global `company_settings` row). `internal/handlers/trips.go` is
now 1,931 lines / 0 SQL sites (was 2,010 / 12), handlers/ total 169 (was 181),
caps lowered in the same PR. Existing trip handler tests (epod, multipart/base64
POD, create/assign flash, selected-trip CRUD, tenant isolation,
complete/detention) pass unchanged; new
`internal/trip/application/trip_read_use_cases_test.go` pins tenant isolation,
the not-found sentinel, stop ordering, POD URL precedence, nullable timestamp
parsing and the legacy best-effort companion lookups.

**Phase 2 progress (2026-11-10, fourth increment - invoices):** the invoice
money/operation paths moved from `internal/handlers/invoices.go` into
`internal/invoice/application`: `InvoiceDocumentQueries` (view due date,
PDF due-date/e-way-bill extras, tenant/bootstrap company profile with the
legacy fallback chain, customer Bill-To, ordered line-item list, HSN master
list/search), `LineItemService` (`AddLineItem`/`EditLineItem`/
`DeleteLineItem` - fail-closed HSN GST resolution, the single paise-rounding
site via `ComputeLineTax`, and insert/update/delete + invoice-header total
recalculation in ONE transaction, every statement tenant-scoped) and
`GenerateIRNUseCase` (invoice/tax-context/line reads, paid/partially-paid and
duplicate-IRN guards, GSTN call delegated through an `IRNGenerator` seam and
the IRN fields persisted). Legacy messages/statuses are preserved with typed
stage errors mapped in the handler (`LineItemWriteError`, `IRNQueryError`,
`IRNStoreError`, `ErrUnknownHSN`, `ErrIRNPaymentStatus`,
`ErrIRNAlreadyGenerated`); PDF/GST money math and template composition stay
in the handler and now compose from the application read models with the same
float arithmetic. `internal/handlers/invoices.go` is now 952 lines / 0 SQL
sites (was 1,259 / 21), handlers/ total 148 (was 169), caps lowered in the
same PR. Existing handler characterization tests (line-item GST fail-closed,
paise rounding, editor lock states, PDF seller block/3-tier titles, view
notes/locked, immutability guards) pass unchanged; new
`internal/invoice/application/invoice_write_use_cases_test.go` pins tenant
isolation (cross-tenant reads/writes invisible), GST split math/rounding,
in-transaction header recalculation, unknown-HSN fail-closed and the IRN
duplicate/status guards.

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
