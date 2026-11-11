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

**Phase 2 progress (2026-11-10, fifth increment - consolidated invoices +
statement of account):** the consolidated (monthly) freight invoicing and
statement-of-account money paths moved from
`internal/handlers/invoices_consolidate.go` into
`internal/invoice/application`: `ConsolidateInvoicesUseCase`
(one transaction: header insert, per-trip annexure freight/toll/detention lines
with per-line paise rounding, header total reconciliation and the detention
`attached` transition; the legacy 100% dynamic credit-policy due-date
precedence with the company-config default fallback; supplier/customer state
derivation with the 9/9/18 GST split; per-financial-year sequential invoice
numbering through the existing sqlite repository with the legacy
random-suffix fallback; typed `ConsolidationError` stages preserving the
legacy log lines and 500 messages) and `CustomerBillingQueries`
(unbilled delivered-trips read model with trip/date filters and live-invoice
exclusion, company default payment terms, statement of account:
invoices/payments/credit-debit-note ledger with aging buckets, WIP unbilled
total, WhatsApp summary/URL and `CleanPhoneForWhatsApp`). The
freight-audit dashboard's recent-trip dropdown query moved to its own slice
(`internal/freightaudit`: `Service.RecentTripRefs`). The handler
keeps HTTP/form-JSON parsing, validation, rendering and the legacy
characterization-test seam; the payload types are re-exported as handler
aliases so template and characterization-test references stay unchanged.
`internal/handlers/invoices_consolidate.go` is now 919 lines / 0 SQL
sites (was 1,674 / 14), handlers/ total 134 (was 148), caps lowered in the same
PR. Existing consolidate/statement/credit-note/GST/freight-audit
characterization tests pass unchanged; new
`internal/invoice/application/customer_billing_use_cases_test.go` pins tenant
isolation (cross-tenant reads invisible, no cross-tenant writes),
intra/inter-state GST math and paise rounding, detention annexure
hours/ref/attachment, due-date precedence, the transaction rollback on a failed
annexure insert, the invoice-number fallback, and the statement aging/ledger
(including credit/debit note rows). Note: credit/debit-note issuance, numbering
and idempotency guards live in `internal/service/credit_note_service.go` +
`internal/handlers/credit_notes.go` (not this file); this increment migrates
only the statement read of notes.

**Phase 2 progress (2026-11-10, sixth increment - drivers):** the driver
identity/KYC/status security surface moved from `internal/handlers/drivers.go`
into `internal/driver/application`: `DriverProfileQueries` (view-page
score/tier, recent behaviour events, recent trips, active preferred vehicle
plus details and the assignment dropdown - every query best-effort like the
legacy handler) and `DriverSelfService` (`GetMe`: tenant-scoped
id-or-user-email identity, the driver_code-falls-back-to-id rule, active-trip
plate and latest-position/snapshot location; `UpdateMe`: tenant-scoped
identity, the legacy CASE update where empty values keep stored columns,
status transitions with the migration-00185 `deactivated_at` clock, and the
best-effort vehicle upsert + telemetry-device link + notes mirror;
`UpdateMyStatus`; `ReportIssue`; `ListMyIssues`) with the
`ErrDriverNotFound` sentinel keeping the handler 404s while DB failures keep
their 500 bodies. The legacy tenant-unscoped identity lookups of the older
mobile status/issue endpoints are preserved verbatim (pinned as frozen
behavior in the new tests), the KYC purge stamp `kyc_purged_at` is never
written by these paths, and license/expiry are still stored verbatim without
an expiry guard (legacy semantics). `internal/handlers/drivers.go` is now
575 lines / 0 SQL sites (was 812 / 22), handlers/ total 112 (was 134), caps
lowered in the same PR. Existing handler characterization tests (KYC status
clock, driver self update, get-me success/not-found/unauthorized,
status+issues API) pass unchanged; new
`internal/driver/application/driver_self_service_test.go` and
`driver_profile_read_test.go` pin tenant isolation, the identity rules
(including the driver_code fallback), status/KYC transitions, tenant-scoped
vehicle binding, verbatim expiry writes, issue insert/list semantics and the
view read-model ordering/limits.

**Phase 2 progress (2026-11-10, seventh increment - vehicles):** the fleet
asset/status/telemetry surface moved from `internal/handlers/vehicles.go` into
`internal/vehicle/application`: `VehicleDetailQueries` (view-page maintenance
flags + override, latest telemetry frame, recent trips, open job cards, assigned
preferred driver + driver-details + assignment dropdown, and the IK01
measuring-point / IK11 measurement / AV command sections; every query
tenant-scoped with the legacy ordering/limits and best-effort empty-section
semantics) and `VehicleCommandService` (migration-00137 command insert with the
legacy `cmd-` UUID id and `acknowledged` status, plus the E_STOP /
HOLD_POSITION / RESUME_MISSION actuation status updates with the legacy
best-effort error swallow and the command-type allow-list). The handler keeps
HTTP parsing, session-derived operator identity, compliance doc-card rendering
and the view/edit templates. `internal/handlers/vehicles.go` is now 769 lines /
0 SQL sites (was 950 / 14), handlers/ total 98 (was 112), caps lowered in the
same PR. Existing vehicle characterization tests (list, CRUD, view branches,
SOP points + measurements, open job cards, status + AV commands) pass unchanged;
new `internal/vehicle/application/vehicle_detail_read_test.go`,
`vehicle_command_test.go` and `vehicle_registration_uniqueness_test.go` pin
tenant isolation of every section (including the same vehicle id under two
tenants), telemetry frame vehicle+tenant binding, the legacy map-key truthiness
(NULL annual estimate/difference/created-at), the AV actuation transitions, and
per-tenant plate uniqueness + fail-closed create validation.

**Phase 2 progress (2026-11-10, eighth increment - driver-vehicle assignment):**
the preferred driver↔vehicle master-assignment surface moved from
`internal/handlers/assignment.go` into `internal/dispatch/application`
(`AssignmentService`): `ActiveByDriver`/`ActiveByVehicle` (tenant-scoped active
row with the legacy sql.ErrNoRows→nil mapping) and
`AssignVehicleToDriver`/`UnassignDriver`/`UnassignVehicle` (the legacy
`driver_preferred_vehicles` SQL verbatim: driver+vehicle tenant-existence
checks, sequential best-effort closing of the conflicting active rows, the
`dva-` UUID insert with `is_primary=1`, and history-preserving `unassigned_at`
stamps). Tenant scoping, the one-active-per-driver/vehicle invariant (partial
unique indexes), reassignment/unassign semantics and the handler's 400/303
contract are unchanged; the handler keeps HTTP parsing/validation, the
auth-derived actor identity and redirects. `internal/handlers/assignment.go` is
now 77 lines / 0 SQL sites (was 152 / 9), handlers/ total 89 (was 98), caps
lowered in the same PR. The legacy `internal/handlers/assignment_test.go` DB
characterization cases moved with the logic to
`internal/dispatch/application/assignment_test.go` (assertions unchanged) and
were joined by focused cases pinning tenant isolation of reads and writes, the
1:1 active uniqueness invariant, the `dva-`/`assigned_by` row shape and the
not-found contract; new HTTP handler tests pin form validation, the
session-derived `assigned_by`, tenant scoping and redirect targets.

**Phase 2 progress (2026-11-10, ninth increment - platform tenant console):**
the platform tenant-administration surface moved from
`internal/handlers/tenants.go` into a new tenant slice,
`internal/tenant/application` (no internal/tenant* domain package existed, so
the handler's own domain got the extracted package): `TenantAdminQueries`
(commercial subscription + plan pricing, per-tenant trip-quota meter, grouped
per-tenant user counts and the provisioning slug/id conflict probe) and
`TenantAdminService` (plan upsert with the legacy 30-day period and the
COALESCE/NULLIF provider-subscription-link preservation, the 14-day trial
extension, the entitlement-override upsert keyed by (tenant, type, key), and
the suspended-tenant session purge). The super-admin RBAC gate
(`tenants:manage`), the bootstrap-tenant suspension guard, form/slug/password
validation, audit logging, JSON/HTML shapes and every status code stay in the
handler; the legacy best-effort error swallows (plan/trial/override writes,
session purge) are preserved verbatim. `internal/handlers/tenants.go` is now
464 lines / 0 SQL sites (was 516 / 8), handlers/ total 81 (was 89), caps
lowered in the same PR. Existing tenant characterization tests (create happy
path / duplicate slug / disabled gate, bootstrap-suspend refusal,
suspend/activate + session purge + audit, RBAC gate, list/fragment render, slug
normalization) pass unchanged; new `internal/tenant/application/admin_test.go`
pins per-tenant subscription/quota/count reads, the slug probe, plan upsert +
provider-link preservation, trial extension scoping, override upsert/isolation,
session-purge scoping and nil-store degradation, and new HTTP handler tests pin
the plan/trial/override mutations (redirect, DB writes, audit) plus validation
messages and the RBAC gate on those routes.

**Phase 2 progress (2026-11-10, tenth increment - public invoice payment
portal):** the login-free /pay/{invoiceId} read model moved from
`internal/handlers/public_payment.go` into `internal/payment/application`:
`PublicPaymentQueries.ResolvePublicInvoice` (invoice id-or-invoice_number
lookup returning the canonical id + its tenant for the Razorpay order/verify
use-cases) and `LoadPublicPay` (invoice + customer/booking/route/trip labels,
line items with the legacy freight fallback row, company branding from
`company_settings` then the invoice tenant name with the hardcoded carrier
defaults, payment history newest-first with the latest settled payment, and
the paid/outstanding state). The portal is public by design - the invoice
id/number in the rate-limited link is the capability - so the legacy queries
stay free of tenant filters and derive the tenant from the invoice row, while
the internal tenant id is still blanked from the view and customer phone/email
are masked in the read model (audit M2). Amount math (outstanding clamped at
zero, paid detection by <=0.001 balance or case-insensitive payment_status),
all JSON/template shapes, the Razorpay gateway use-cases and every
400/404/500 status/message stay unchanged; the handler keeps HTTP, handler
wiring, the ?paid=true success flag, format/lang negotiation and JSON/HTML
rendering. There is no token-expiry/invoice-status gate in this legacy
surface (pinned by test). `internal/handlers/public_payment.go` is now 305
lines / 0 SQL sites (was 598 / 7), handlers/ total 74 (was 81), caps lowered
in the same PR. Existing characterization tests (unpaid/paid HTML+JSON,
not-found, server-side order + paid-invoice rejection, valid/fake signature
settlement/idempotency, missing fields, PII masking) pass unchanged; new
`internal/payment/application/public_payment_test.go` pins invoice
id/number resolution, tenant derivation + branding override, masked PII,
amount/outstanding/paid-state math including the overpayment clamp and the
legacy uppercase-PAID quirk, payment-history ordering/latest, the fallback
line item and company defaults, and cross-invoice line-item/payment
isolation.

**Phase 2 progress (2026-11-10, eleventh increment - tenant data purge):** the
destructive per-tenant operational purge moved from
`internal/handlers/tenant_purge.go` into `internal/privacy/application`
(`TenantPurgeService`: `Preview` dry-run counts and `Execute` in one
transaction), keeping the legacy scope verbatim - the 18 global-predicate
tables deleted through trip/driver/vehicle/parent joins and the 78 direct
`tenant_id` tables deleted children-before-parents, with portable
`appdb.Rebind`/`appdb.IsPostgres`, the sqlite-only
`PRAGMA foreign_keys = ON`, table-existence probes, per-table deleted
counts, the post-purge leftover check and rollback-on-failure. The
fail-closed protected-table guard (tenants/users/roles/billing
seeds/migration bookkeeping), empty-id and bootstrap-tenant guards, the
unknown-tenant message, preview-vs-execute semantics and idempotent
re-execute are preserved verbatim. The handler keeps the platform-only
`tenants:manage` route gate, typed-confirm validation, HTTP 400/200 JSON
shapes/messages and the `tenant.purge` audit write.
`internal/handlers/tenant_purge.go` is now 56 lines / 0 SQL sites (was 287 /
7), handlers/ total 67 (was 74), caps lowered in the same PR. Existing purge
characterization test (preview counts/no delete, wrong confirm, bootstrap
refusal, execute deletes ops and keeps users/tenant) and the tenants RBAC
route-gate test pass unchanged; new
`internal/privacy/application/tenant_purge_test.go` pins preview-vs-execute,
tenant isolation of the purge and of the other tenant's identical chain,
FK-safe ordering with foreign_keys ON (children including global-predicate
rows deleted before their parent trip/driver/vehicle), protected
users/tenants survival, the bootstrap/empty/unknown-tenant guards and
idempotent re-execute.

**Phase 2 progress (2026-11-10, twelfth increment - route optimization jobs):**
the route-optimization job persistence/read surface moved from
`internal/handlers/routes.go` into the new `internal/route/application`
(`RouteOptimizationService`: `VehiclesForOptimization` - up to 50
non-blocked tenant vehicles ordered by plate for the optimizer dropdown;
`CreateJob` - the uuid-keyed pending `route_optimization_jobs` insert with
input JSON, provider and creator; best-effort `MarkProcessing`/
`MarkFailed`/`MarkCompleted` status+result+timestamp updates; `RecentJobs`
- the tenant's 20 newest jobs with the legacy per-row scan tolerance; and
`Job` - the tenant-scoped polling lookup whose unknown/foreign id surfaces
`sql.ErrNoRows` for the handler 404). The handler keeps HTTP/JSON parsing,
`optimizer.Validate` + max-50-shipments validation, provider/config
selection (mock/osrm-public/osrm-selfhost + OSRM URL), the synchronous solve
call, every JSON shape/status code (202/422/500/404) and the `writeJSON`
helpers; route master-data CRUD still delegates to `h.Services.Routes`
(unmoved, validation/uniqueness unchanged). Tenant scoping is preserved
verbatim (every job statement filters tenant_id; a foreign-tenant job id
reads as not-found) and the legacy best-effort status-update semantics
(errors ignored) plus SQL NULL-vs-empty-string validity in the polling
payload are unchanged. `internal/handlers/routes.go` is now 335 lines / 0
SQL sites (was 383 / 7), handlers/ total 60 (was 67), caps lowered in the
same PR. Existing route-optimize characterization tests
(`TestRouteOptimize_MockProvider`, `TestRouteOptimize_Validation`) pass
unchanged; new `internal/route/application/optimizer_jobs_test.go` pins
tenant isolation of the vehicle lookup and job reads (foreign-tenant id ->
not-found), blocked-vehicle exclusion + ordering/limit, the
pending->processing->completed/failed write lifecycle with timestamps,
recent-jobs ordering/20-row cap/NULL completed_at omission, and the legacy
NULL-vs-empty-string polling semantics.

**Phase 2 progress (2026-11-10, thirteenth increment - driver SOS/emergency):**
the driver SOS trigger surface moved from `internal/handlers/sos.go` into the
new `internal/alerts/application` (`SOSService`: `DriverForUser` -
id-or-user-email driver identity with the session-id fallback, no tenant
filter by design so an emergency is never dropped; `DriverTenant` - the
header-auth driver's own-org emergency attribution; `ActiveTrip` - newest
active trip/vehicle for omitted payload fields; `NewSOSID` - the
sos_<command_id>/sos_<uuid> id rule; and `Trigger` - the idempotent
`outbox_events` insert (`ON CONFLICT DO NOTHING`, legacy payload map keys
verbatim) that only on first delivery publishes the `driver.sos_triggered`
event through the shared EventBus and appends the
`driver_sos_triggered`/`safety` `audit_logs` row). The handler keeps HTTP
session/header auth, JSON parsing, the triggered_at/occurred_at/now defaults,
the reason default, the 201-created vs 200-idempotent status and the
`SOSResponse` JSON. Legacy semantics preserved: the tenant comes from the
request context or, for header-auth drivers, from the driver's own tenant
(never another org); the outbox insert is keyed by aggregate id so duplicate
command ids re-alert nobody, and insert failures still render as a duplicate
acknowledgment (rowsAffected 0 => 200) with no publish/audit. The old
`App.logAuditDirect` helper moved with the write into the service.
`internal/handlers/sos.go` is now 165 lines / 0 SQL sites (was 221 / 5),
handlers/ total 55 (was 60), caps lowered in the same PR. Existing
end-to-end SOS characterization test (tenant/driver/vehicle/trip seed,
header-auth trigger, outbox payload, alert-pipeline blocker alert + channel
dispatch, 60s vehicle cooldown/occurrences dedup) passes unchanged; new
`internal/alerts/application/sos_service_test.go` pins the identity rules
(including the unresolvable-session fallback), own-org emergency tenant
attribution, active-trip status/order selection, first-delivery-only
publish+audit with command-id idempotency, the `TenantOrPlatform` audit
fallback, nil-bus degradation and the insert-failure duplicate-ack path.

**Phase 2 progress (2026-11-10, fourteenth increment - E-Way Bill pages):** the
E-Way Bill presentation read model moved from
`internal/handlers/ewaybill.go` into the existing `internal/ewaybill` slice
(the domain's service/application package, no subdirectory - same pattern as
`internal/sharing`/`internal/fleetreports`): `EWayBillReadQueries` with
`Stats` (list-page counters: total / active-with-vehicle / Part-A-only /
extended / cancelled / expired), `Items` (100 newest bills + trip-number
LEFT JOIN with NULL coalescing and per-row scan tolerance), `CandidateTrips`
(50 newest trips in the five EWB-eligible statuses joined to routes) and
`Events` (chronological `eway_bill_events` history with payload/actor NULL
defaults). The handler keeps HTTP/permissions (`ewaybill:*`), form/JSON
parsing, the lifecycle calls into the untouched `EWayBillService`
(tenant-gated generate/attach/extend/cancel with its provider seam,
idempotency and status transitions), all status codes/messages/JSON shapes
and the page/partial/detail templates; the page view models are re-exported
as handler aliases so template and render-test references stay unchanged.
The presentation reads keep their FROZEN legacy global scope: `eway_bills`
has no tenant_id and the legacy handler never joined `trips.tenant_id`, so
the list/stats/candidate-trip/event views are global for any operator with
`ewaybill:read` - preserved verbatim and flagged for a follow-up security
increment, while the write lifecycle keeps its existing tenant gate
(`TestGeneratePartA_CrossTenantDenied`). `internal/handlers/ewaybill.go` is
now 371 lines / 0 SQL sites (was 487 / 5), handlers/ total 50 (was 55), caps
lowered in the same PR. Existing characterization tests
(`TestSettlementUI_PagesAndEWayBillCard` - trip EWB partial + mock-adapter
generate redirect; `TestAllTemplatesRenderCleanly` - list/detail/table
templates with the moved view models) pass unchanged; new
`internal/ewaybill/read_queries_test.go` pins the stats math, 100-row list
window + ordering + NULL coalescing, candidate-trip eligibility/ordering/
50-row window, chronological event history, nil-DB/missing-table degradation
and the frozen global scope.

**Phase 2 progress (2026-11-10, fifteenth increment - Command Center inline
EWB extend):** the last raw-SQL surface of `internal/handlers/console.go`
moved into the existing `internal/controltower/application` (the Phase-1
Command Center slice, extended rather than duplicated):
`ExtendEwayBillUseCase` (`Extend` - tenant-scoped `eway_bills JOIN trips`
lookup with `ErrConsoleNotFound` for unknown/foreign ids, the local
`valid_until + hours` shift, the `EWB_EXTEND_ENABLED` provider seam that
consults the integration `Client.Extend` with reason
`console_inline_extend` and lets a successful provider validity win while
provider errors fall back to the local shift, the validity UPDATE, the
best-effort `EXTENDED` `eway_bill_events` insert - error logged exactly as
before - and the `audit_logs` write) plus `WriteAuditLog` (the legacy
direct audit helper moved out of the handler; still exposed through
`handlers.WriteAuditLog`/`writeAuditLog` as thin wrappers so the dispatch
planner presentation keeps its `legacyhandlers.WriteAuditLog` calls). The
handler keeps the JSON body decode/1 MB cap, the 1..24h validation and its
exact 400 message, the 404/internal `httpx.Error` mapping, the nil-DB guard
and the `{"ok":true,"new_expiry":...}` JSON. Tenant scoping, event/audit
best-effort semantics, actor id and the `TenantOrPlatform` audit-tenant
fallback are unchanged. `internal/handlers/console.go` is now 321 lines / 0
SQL sites (was 362 / 5), handlers/ total 45 (was 50), caps lowered in the
same PR. Existing console render characterization test passes unchanged; new
`internal/controltower/application/ewaybill_extend_test.go` pins tenant
isolation of the lookup (foreign/unknown id -> not-found with no write), the
local-shift vs provider-enabled/error/nil-client seam matrix, the EXTENDED
event + audit row fields, the audit tenant fallback and the exported
audit-writer row mapping; new HTTP tests pin the validation status codes
(0/25/negative/non-JSON -> 400), unknown/cross-tenant -> 404, the success
JSON + DB/event/audit writes and the provider-enabled override.

**Phase 2 progress (2026-11-10, sixteenth increment - shipper portal feedback,
brand and last-seen):** the remaining raw-SQL surfaces of
`internal/handlers/customer_portal.go` moved into the existing
`internal/customerportal` slice (extended; Phase-1 read models already live
there): `FeedbackService.Create` (customer_users-scoped trip ownership
lookup with the legacy 404-vs-403 discrimination - trip absent in the tenant
=> not-found, existing tenant trip not linked to the shipper => forbidden -
the legacy "no such table" unscoped customer-derivation fallback, the uuid
feedback insert with the legacy UTC timestamp format, the lazy
`CREATE TABLE IF NOT EXISTS trip_feedback` dev/test fallback with retry, and
the legacy `failed to save feedback: <err>` wrapping), `TenantBrand`
(tenant_company_profiles first, global `company_settings` id=1 second,
"Avandab" default last, preserving the shared-scan-buffer quirk where a
tenant name suppresses the global logo) and `LastSeenFix` / `ParseTimeFlex`
(newest telemetry fix ordered by `ts_unix` epoch, never the mixed-format
`timestamp` text; nil result for an unreported vehicle; query failures
surfaced for logging, never rendered as "no data"). The handler keeps
JSON/form parsing, trip_id/rating/comment validation and its exact
400/403/404/500 bodies, the ETA fallback, multi-stop/tracking rendering and
thin `lastSeenFix`/`parseTimeFlex`/`tenantBrand` wrappers so the existing
characterization tests (last-seen ordering/no-rows/failure, brand resolution,
white-label rendering, portal invoice scoping, tracking JSON) keep their
signatures; JSON/HTML shapes and the `withBrand` template injection are
unchanged. `internal/handlers/customer_portal.go` is now 477 lines / 0 SQL
sites (was 586 / 9), handlers/ total 36 (was 45), caps lowered in the same
PR. Existing characterization tests pass unchanged; new
`internal/customerportal` tests pin the feedback ownership matrix, tenant
isolation, the frozen no-duplicate-guard behavior, the lazy-table and
missing-table fallbacks, insert-error wrapping, the brand resolution chain +
logo quirk, and last-seen epoch ordering/no-rows/failure/parse formats.

**Phase 2 progress (2026-11-10, seventeenth increment - maintenance domain
complete):** the remaining raw SQL of the maintenance/tyre/inventory web
surfaces moved into the existing `internal/maintenance` slice (extended, no
new package): `VehicleLabels` (tenant vehicle-id -> display-label map for the
job-card panel, registration -> vehicle-number -> id fallback),
`VehicleOptions` (tenant vehicles ordered by vehicle_number for the tyre-mount
and inventory issue/return dropdowns), `Facilities` (tenant facilities
ordered by name for stock operations), `VehicleLabel` (single id-only vehicle
label for the work-order detail page, best-effort like the legacy handler and
tenant-trusted because it runs after the tenant-scoped work-order read) and
`WriteOverrideAudit` (the `maintenance_override` audit_logs row with the
legacy `fmt.Sprintf` payload bytes, action/table, actor id and
`TenantOrPlatform` tenant fallback; failures stay best-effort at the call
site). The handlers keep rendering, the map-shape adaptation used by the
templates, every status code/message, and the untouched repo/worker/
plan-scheduler lifecycle paths; `internal/handlers/maintenance.go` is now
1,081 lines / 0 SQL sites (was 1,103 / 3), `maintenance_plans.go` 898 / 0
(was 910 / 2) and `work_orders_web.go` 139 / 0 (was 141 / 1), handlers/ total
30 (was 36), caps lowered in the same PR. Existing maintenance/tyre/
inventory/work-order characterization tests (dashboard render, schedule
creation/evaluation, record/resolution, override, RBAC, work-order web/API
lifecycle + close, tyres/inventory lifecycle + RBAC) pass unchanged; new
`internal/maintenance/web_queries_test.go` pins tenant isolation of both
dropdowns and the label map, the registration/vehicle-number/id fallbacks,
ordering, best-effort nil-DB/dead-table behavior, the id-only label quirk and
the audit row shape/payload bytes + platform-tenant fallback.

**Phase 2 progress (2026-11-10, eighteenth increment - expenses/kharcha
domain complete):** the remaining raw SQL of the driver expense, e-POD and
driver-money surfaces moved into a new `internal/kharcha/application` slice
(no `internal/kharcha*`/`internal/expense*` package existed, and
`internal/payment` owns Razorpay/recorded payments, not the expense ledger):
`LookupUserDriver` (the legacy mobile identity rule - drivers.id OR
drivers.email via the users table, no tenant filter, nil for no driver row,
real errors surfaced for logging), `UpdateTripPODArtifacts` (the best-effort
trips e-POD signature/discrepancy UPDATE with the exact
COALESCE/NULLIF/zero-guard CASE semantics), `DriverSettlements` (the
driver-scoped 50-newest settlement read with the legacy trip_id->"period"
mapping and nullable paid_at) and `ExpenseOCRFields` (tenant-scoped
ocr_amount/ocr_confidence read where missing rows/NULL columns stay JSON
nulls). The approval/verification state machine, fuel-audit enforcement,
ledger/stats math, driver balance/advance decisions, audit helper and all
validation stay in the untouched `service.KharchaService`,
`service.KharchaVerifyService`/`service.DriverBalanceService` and handler
code; handlers keep multipart/JSON parsing, status codes, response shapes and
rendering. `internal/handlers/kharcha.go` is now 505 lines / 0 SQL sites (was
516 / 3), `driver_money.go` 160 / 0 (was 177 / 2), `kharcha_verify.go` 61 /
0 (was 65 / 1), handlers/ total 24 (was 30), caps lowered in the same PR.
Existing POD/expense-API characterization tests
(`TestDeliverPOD_SuccessAndErrors`, `TestDeliverPOD_OTPVerification`,
`TestCreateExpenseAPI_SuccessAndErrors`) pass unchanged; new
`internal/kharcha/application/kharcha_test.go` pins the identity rules
(email/id match, missing row, surfaced lookup errors, no tenant filter), the
e-POD coalesce/zero-guard matrix, settlement ordering/50-row cap/period
mapping/nullable paid_at/driver isolation, and tenant-scoped OCR field reads
with NULL/no-row semantics.

**Phase 2 progress (2026-11-10, nineteenth increment - telemetry/devices
domain complete):** the remaining raw SQL of the telemetry-device surfaces
moved into their existing slices: `internal/mqttservice` got
`CredentialStore` (`BrokerUsername` - the tenant-scoped users/drivers LEFT
JOIN resolving the driver_id or falling back to the auth user id;
`HashByUsername` - the stored mosquitto hash read where sql.ErrNoRows means
"not provisioned yet"; `Upsert` - the legacy
INSERT ... ON CONFLICT(username) rotation that also provisions a
rotated-but-missing row and stamps rotated_at) and `internal/telemetry` got
`FleetVehicles` (the EV-configuration modal's tenant vehicle list ordered by
vehicle_number, best-effort) and `MarkVehicleElectric` (the best-effort
`fuel_type='electric'` transition after a successful EV profile write).
Secret hygiene is unchanged and now explicit in the store contract: only the
PBKDF2 mosquitto hash reaches `driver_mqtt_credentials`; the plaintext
minted by mqttservice.NewBrokerSecret is returned by the handler exactly once
and is never logged or persisted. The handlers keep auth/tenant gating,
wildcard-username validation (422), the provisioned-vs-rotated response shape,
every status code/message and the EV profile/charging rendering.
`internal/handlers/mqtt_credentials.go` is now 135 lines / 0 SQL sites (was
162 / 3) and `telemetry_devices.go` 700 / 0 (was 707 / 2), handlers/ total 19
(was 24), caps lowered in the same PR. Existing MQTT credential
characterization tests (first call provisions then stops repeating the
secret, rotate replaces the hash, auth-user fallback, wildcard rejection,
tenant scoping, rotate-without-row provisioning) plus the telemetry-device
RBAC/tenant guards and EV API suite pass unchanged; new
`internal/mqttservice/credential_store_test.go` pins the tenant-scoped
username resolution/fallbacks, upsert rotation + rotated_at stamp, hash-only
secret hygiene and nil-DB behavior, and new
`internal/telemetry/vehicle_fleet_test.go` pins tenant-scoped ordering,
best-effort degradation and the tenant-scoped electric transition.

**Phase 2 progress (2026-11-11, twentieth increment - contact + compliance
domains complete):** the remaining raw SQL of the public support/grievance
inbox and the compliance dashboard moved into two new slices (no
`internal/contact*`, `internal/crm` or `internal/compliance*` package
existed; `service.ComplianceService`/`ComplianceRadarService` stay
untouched): `internal/contact/application` (`Service.Submit` - uuid +
AVN-XXXXXXXX public reference and the pending contact_submissions INSERT;
`FindForRequester` - ticket number + submitter-email lookup with the legacy
single not-found message; `UpdateStatus` - status write whose first touch
stamps acknowledged_at exactly once via COALESCE, rows-affected drives the
handler 404; `List` - newest-first cap-100 list with the handler-validated
status filter) and `internal/compliance/application`
(`DashboardQueries.Overview` - drivers/vehicles expiry aggregation, blocked
flag/status rules with the legacy expanded default reasons and expiry-reason
precedence, 7-day expiring-soon counters, and pending driver/vehicle document
counts tenant-scoped through the owner entity). Tenant scoping is preserved
exactly: the compliance dashboard scopes org callers while the bootstrap
tenant ('1'/empty: single-tenant + platform views) keeps the legacy global
aggregation; the contact inbox is company-global by design (no tenant_id) and
remains so. The handlers keep form/JSON parsing, required-field and
status-whitelist validation, the 48h-ack/1-month SLA derivation, safe
redirects, HTTP/JSON/HTML shapes and every status code; the dashboard view
models are re-exported as handler aliases so template coverage tests are
unchanged. `internal/handlers/contact.go` is now 273 lines / 0 SQL sites (was
346 / 4) and `compliance.go` 329 / 0 (was 465 / 4), handlers/ total 11 (was
19), caps lowered in the same PR. Existing contact SLA/ack-once/list-filter
and compliance tenant-isolation characterization tests pass unchanged; new
`internal/contact/application/tickets_test.go` pins submission/lookup,
first-touch acknowledgement, ordering/filter/cap and the global-inbox scope,
and new `internal/compliance/application/dashboard_test.go` pins tenant
isolation vs bootstrap-global, blocked/expired/expiring-soon rules, reason
precedence/defaults and pending-document scoping.

**Phase 2 FINAL (2026-11-11, twenty-first increment - final sweep, handlers/ at
ZERO raw SQL):** the last 11 raw-SQL call sites across 10 handler files moved
into owning slices, completing Phase 2. Baseline at 41345ca was 245 sites in
handlers/; it is now 0. Packages picked (new packages only where no slice
existed): **new** `internal/search/application` (whole cross-entity search
engine - specs/queries/tenant scoping/LIKE escaping/row scanning behind a
permission callback, so the handler keeps only AuthSrv gating, JSON/HTML
shaping); **new** `internal/outbox/application` (mobile outbox batch:
single-Tx insert with idempotency-key primary keys and typed
unavailable/write-failed errors); **new** `internal/backhaul/application`
(backhaul page trip selector); extensions of existing slices:
`internal/vehicle/application.VehicleOptions` (shared tenant vehicle
dropdown for the video-telematics and cold-chain modals),
`internal/tenant/application.SyncTenantDisplayName` (onboarding wizard
display-only sync, best-effort),
`internal/auth.MarkPhoneVerified` (OTP possession stamp, first stamp wins),
`internal/customer/application.LinkPortalUser` (idempotent customer_users
link), `internal/booking/application.BoardQueries` (kanban board read model;
lane mapping/rendering stays in the handler) and
`internal/availability.LoadIncidents` (status-page incident feed with the
legacy 10-minute de-duplication and nil-on-error behavior). Every handler
keeps HTTP parsing/validation/auth/permission closure, status codes/messages,
JSON/HTML shapes and rendering; tenant scoping, rate limiting, idempotency,
money/fuel math, lifecycle/state rules and audit behavior are preserved
verbatim. Per-file SQL caps are now all 0; the per-file line caps were added
for the swept files (search.go 112, settings.go 576, scorecard.go 670,
outbox_batch.go 65, otp.go 135, fuel_issues.go 538, failpage.go 315,
customers.go 416, bookings.go 458, backhaul.go 246) and **the total assertion
was tightened to `assert.Zero(totalSQL)`** so handlers/ can never grow raw
SQL again (0 is the invariant; no cap to grow into). Existing characterization
tests (search grouping/permission-gating/API tenant scoping, outbox
persistence/idempotent replay/503/400, OTP verify stamping + no-enumeration,
status page HTML/JSON + measured uptime, cold-chain + video dashboards and
RBAC, bookings board render/a11y/status lanes, portal-access grant idempotency,
backhaul e2e, settings/onboard) pass unchanged; new focused tests pin search
permission/tenant scoping + LIKE escaping, outbox batch idempotency, vehicle
dropdown scoping, tenant-name sync, phone-verified first-stamp, portal-link
idempotency, board tenant/active-trip join, availability incident
filter/de-dup and backhaul trip eligibility.

**SECURITY FIX (2026-11-11) - E-Way Bill presentation reads are now
tenant-scoped:** the `/ewaybill` list page and its presentation read model
(`internal/ewaybill/read_queries.go`) previously ran global queries -
`eway_bills` has no tenant_id column and the legacy handler never joined
`trips.tenant_id` - so any operator holding `ewaybill:read` could see every
tenant's EWB numbers, statuses, goods values, candidate trips and event
history. That was a pre-existing cross-tenant data exposure, not frozen
behavior worth preserving. `Stats`, `Items`, `CandidateTrips` and
`Events` now take the caller's tenant (extracted in the handler exactly like
the rest of the authenticated surface: `shared.TenantIDFromContext`) and
scope through the owning trip: stats/list via an inner
`JOIN trips ... AND t.tenant_id = $n`, candidate trips via
`WHERE t.tenant_id = $n`, and event history via
`eway_bill_events -> eway_bills -> trips.tenant_id`. The read model FAILS
CLOSED: an empty or unknown tenant returns zeroed stats and empty lists -
there is no global fallback and no DefaultTenant substitution. Orphan EWBs
whose trip row is deleted/unknown are hidden from every tenant (inner join).
No other handler behavior changed: same statuses, messages, JSON/HTML shapes,
ordering, limits and best-effort degradation. Tests: the read-model suite was
rewritten from the old global-scope expectations to tenant isolation (tenant A
cannot see tenant B rows, orphan EWB invisible, empty tenant fails closed) and
a handler-level regression test
(`internal/handlers/ewaybill_read_isolation_test.go`) seeds two tenants and
proves the rendered list only contains the caller's EWB rows and candidate
trips. Follow-up (same fix, 2026-11-11): the detail record lookup is
tenant-scoped too. New `EWayBillService.GetByNumberForTenant` /
`GetByTripForTenant` join `trips` and filter `t.tenant_id`; they fail
closed on an empty tenant, and unknown vs cross-tenant ids both return
`ErrEWBNotFound` so the detail page 404 body is identical (no existence
oracle). `handlers/ewaybill.go` Detail and GetForTrip, the trips-page EWB
card (`handlers/trips.go`) and the agent `extend_ewaybill` tool use the
scoped lookups (the trips-page card is also tenant-checked upstream; the
scoped read is belt-and-braces). The write path is tenant-gated as well:
`AttachPartB`, `Extend` and `Cancel` read the caller tenant from ctx and
require the EWB to belong to that tenant before any provider call
(cross-tenant => `ErrEWBNotFound`, no mutation); their post-write response
reads use the scoped lookup. Trusted background callers carry no request
tenant and keep the legacy by-number path, but now inject a tenant: the
trip-event auto-generator derives the event/trip-row tenant into ctx before
`AttachPartB`, and the expiry monitor injects each row's tenant before
`Extend`/`Cancel`, so the gate applies there too. Base `GetByNumber`/
`GetByTrip` remain for trusted internal callers/tests only; the inbound
integration API (`internal/integration`) keeps using the integration client
under `RequireAPIAuth` tenant middleware. No operator-facing unscoped read
remains. Handler-level regressions now also prove tenant A detail requests for
tenant B's EWB return the same 404 body as an unknown number (own record still
renders with its events) and tenant A trip-card requests for tenant B's trip
return the same 404 as an unknown trip.

**Phase 3 progress (2026-11-11, first increment - agent tools zero SQL):**
`internal/agent/tools.go` (1,186 lines at `41345ca`) now has ZERO raw SQL -
the last inline query, in the `get_open_alerts` handler, moved into the
owning alerts slice as
`internal/alerts/application.OpenAlertQueries.List` (tenant-scoped
`alerts WHERE status='open' AND tenant_id = ?` with optional severity
filter, newest-first legacy LIMIT 20, nullable entity_* columns preserved via
pointer scans, JSON tags byte-compatible with the legacy tool output). The
tool now has the ADR Phase 3 shape - schema + argument mapping + delegate -
and keeps the exact model-facing strings ("database not available",
"No alerts found: <err>", "No open alerts found."), the tenant extraction
from ctx (`shared.TenantIDFromContext`), the `alerts:read` Resource/Action
gate and unchanged RegisterTools wiring; the 2026-09-17 cross-tenant
regression test (`TestGetOpenAlerts_TenantIsolation`) passes unchanged. The
other 24 tools already delegate to the legacy `service.Services` container,
maintenance repository, ewaybill service or dispatch application facade -
mapping each to a domain application facade remains for later Phase 3
increments. Ratchet: agent/tools.go 1 -> 0 SQL and line cap 1186 -> 1152 (the
handlers zero-SQL invariant is untouched). New
`internal/alerts/application/open_alerts_test.go` pins tenant isolation,
open-only + severity filtering, ordering/limit, nullable entity fields, the
JSON wire shape and error mapping.

**Phase 3 progress (2026-11-11, second increment - work-order tools):** the
four maintenance job-card tools (`list_work_orders`, `get_work_order`,
`create_work_order`, `transition_work_order`) no longer touch the maintenance
SQL repository: the `maintenanceRepo`/`maintsql` plumbing left `tools.go`
and the tools delegate to the new `internal/maintenance.WorkOrderFacade`
(`Create`/`List`/`Get`/`Transition`). The facade owns store wiring, tenant
derivation from ctx, creation defaults (uuid, status open), the legacy
assign-then-transition ordering and the done side effects
(`CompleteWorkOrder` service-record write + due-flag resolution); a missing
store fails closed with `maintenance.ErrStoreUnavailable` ("maintenance store
unavailable" - the same model-facing string). Tools keep their JSON schemas,
validation ("vehicle_id and title are required", "unknown job card status %q",
"due_at must be RFC3339"), RBAC tags (`maintenance:read/create/update`),
approval-gate wiring, exact JSON output shapes ("No job cards found",
"job card %s not found", "Job card %s closed as done; service record written.",
"Job card %s moved to %s.") and the `workOrderJSON` field set.
`internal/agent/tools.go` is now 1,141 lines / 0 SQL (was 1,186); the line
cap was lowered to 1141 in the same PR. Existing work-order characterization
tests (lifecycle, RBAC tags, approval gating) pass unchanged; new
`internal/maintenance/work_order_facade_test.go` pins tenant isolation,
lifecycle transitions including the service-record side effect, not-found and
store-unavailable mapping, and new `TestWorkOrderTools_TenantIsolation` pins
cross-tenant invisibility through the tool layer.

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
