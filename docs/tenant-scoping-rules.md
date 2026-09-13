# Tenant scoping rules (Avandab / MVTMS)

Operational rules discovered and enforced while hardening the EWB/invoice path.
Source of truth for anyone touching tenant-scoped lookups.

## 1. Dual-path rule: authorization is strict, enrichment is tolerant

A lookup can serve two different purposes, and they need opposite failure modes:

- **Authorization** (may this caller act on this row?) — strict, fail closed.
  A missing row and a cross-tenant row must be indistinguishable to the caller
  (both "not found"), so a tenant cannot probe for another org's IDs.
- **Enrichment** (fill in optional related data) — tolerant. A trip with no
  linked booking/route/customer is a data-completeness gap, not a security
  failure, so `sql.ErrNoRows` falls back to caller-supplied values.

Conflating the two is a real regression source: making enrichment strict broke
invoice→EWB convergence and the phase-4D integration suite (the seeded trips
have no `booking_id`, so the enrichment JOIN returned no rows). Any *other*
error (missing table, bad schema) still fails hard — silently stamping a
government document from partial tax context is not acceptable.

Reference implementation: `internal/ewaybill/service.go` `GeneratePartA`,
steps "1a. Authorization gate" and "1b. Enrich from related rows".

## 2. company_settings vs tenant_company_profiles

- `company_settings` (id=1) is the **platform-global default singleton**,
  created once at migration `00042`. It is never dropped or duplicated.
- `tenant_company_profiles` (migration `00125`) holds per-tenant GSTIN /
  state_code / prefixes, PK = `tenant_id`, with FK triggers to `tenants(id)`.
- Read order: **tenant row first, global singleton as fallback**. The fallback
  is allowed only for tenant `'1'` or an empty tenant — a *different* tenant
  must never inherit the global values (that would stamp org A's GSTIN on org
  B's document).
- Migration `00125` deliberately excludes tenant `'1'` from its backfill so
  single-tenant deployments and tests that write `company_settings id=1`
  keep working. New tenants start with NO row (blank profile forces
  `/company/onboard`).

## 3. MustTenantID: non-request paths only

- `shared.MustTenantID(ctx)` panics when the tenant is absent. Appropriate for
  background jobs / cron entrypoints where a missing tenant is a programmer
  error.
- On an HTTP request path a missing tenant means a bad/expired session, so a
  panic becomes a **500** via `Recoverer` instead of a clean **401**. Use
  `shared.TenantRequired(ctx)` and answer 401 (see `requireTenant` helpers in
  `internal/handlers/share.go`, `fuel_cards.go`, `ops_errors.go`).
- The security gate blocks *new* `MustTenantID(` usage on request paths;
  pre-existing call sites are grandfathered by the LINT_BASE ratchet and are a
  quantified backlog (each handler owns its own error contract).

## 4. Automated gates

- `scripts/security-check.sh` — fails on literal tenant assignments
  (`TenantID: "1"`, `TenantID: shared.TenantID("1")`, unmarked
  `shared.DefaultTenant`) in changed Go files, and on new `MustTenantID(` in
  handler packages. `internal/shared/tenant.go` is the sanctioned home of the
  `DefaultTenant` definition and is excluded, matching `tenant-lint.sh`.
- `scripts/tenant-lint.sh` — SQL-level checks: raw SQL touching a tenant table
  without `tenant_id`.
- Sanctioned escape hatches: `//nolint:tenant-default` (bootstrap/global scope,
  with justification) and `shared.WithGlobalScope(ctx)` for system jobs.

## 5. Backup paths use different DB filenames on purpose

- **Device / ADB path**: `mvtms.db` at `/data/local/tmp` →
  `backup_db.sh` (host `adb pull`).
- **Repo-local path**: `transport.db` → `scripts/backup-db.sh` (online
  `sqlite3 .backup` + Cloudflare R2 sync, 7-day local retention).
- `scripts/backup-avandab-db.sh` is a **deprecated wrapper** forwarding to
  `scripts/backup-db.sh`; it previously hardcoded `/proc/3710/root/...`, which
  broke whenever the process id changed.
- Never `cp` a live WAL-mode SQLite file for a backup; use the online backup
  API.
