# Multi-Tenant Onboarding — Implementation Spec v1

Status: ready
Depends-on: 00012_rbac.sql (`sync_user_role_on_*` triggers), 00065 tenant backfill
           (guarantees all existing rows `'1'`), shared.TenantIDFromContext fail-closed contract
Migration owner: db/migrations/00102_tenants.sql   (reserved exactly here in 00-migration-ownership-index.md)

---

## 0. Verified ground truth (file:line proofs of current state)

Run these greps yourself before coding; every claim below is reproducible.

### 0.1 Resolver returns bootstrap tenant `'1'`, wired everywhere

`internal/middleware/api_auth.go:25-29` — `DefaultTenantResolver` ignores its inputs and
returns `'1'`; it is wired as the default at `api_auth.go:34` and `internal/middleware/middleware.go:148`,
and injected at every mount point in `cmd/main.go:738,851,906,930,1098`.

### 0.2 Bearer claims hardcode the tenant

`internal/auth/presentation/api/handlers/auth_handler.go:71,127` — API-token issue/validation
stuffs a hardcoded `tid` into claims; downstream code treats it as authoritative.

### 0.3 Casbin has no tenant dimension — isolation = query filters only

`internal/auth/casbin.go:26` — matcher/model carries no domain dim. Tenant separation today
exists **only** where individual SQL queries filter on `tenant_id`. RBAC grants are global.

### 0.4 Role writes are memory-only; DB persistence rides triggers; handlers swallow errors

- `AddRoleForUser` mutates Casbin memory only; durable persistence comes from the
  `sync_user_role_on_insert/update/delete` triggers — `db/migrations/00012_rbac.sql:125-153`.
- Handlers discard the error: `_ = h.AuthSrv.AddRoleForUser(...)` at
  `internal/handlers/users.go:115,164` and `internal/handlers/auth.go:182`.

### 0.5 Leak inventory — unscoped / pinned queries that must become tenant-scoped

| Area | Evidence | Problem |
|---|---|---|
| customers | `db/query/customers.sql:6,:17,:20,:27,:32,:36` — 6 queries | zero `tenant_id` predicate |
| kharcha raw SQL | `internal/service/kharcha_service.go:72,:134,:158,:222,:233,:248,:258,:298,:405,:419,:450-467` — 11 stmts | raw SQL, no tenant filter |
| kharcha pin | `kharcha_service.go:324` | explicit `shared.DefaultTenant` pin |
| fuel audit | `internal/service/fuel_audit_service.go:116,:171,:322,:483,:498,:532,:590(×4)` | 7 statements unscoped |
| verify | `internal/service/kharcha_verify.go:39` | unscoped |
| FASTag reconcile | `internal/service/fastag/reconcile.go:30+:91+:160` | unscoped |
| drivers email lookup | `db/query/drivers.go:363-388+404` | email subquery crosses tenants |
| users list | `SearchUsers`/`CountUsers` + `users_daterange.go` | zero filter; SELECT includes `password_hash` |
| dashboard | `dashboard_service.go:46-67,268-271` | process-wide 3s cache shared across tenants |

### 0.6 Settings singleton forces an overlay design

`company_settings` is `CHECK(id = 1)` — physically single-row. Per-tenant settings therefore
overlay via the existing `company_config` key/value decision (§5.7), not new columns.

### 0.7 share_links has no tenant column

`db/migrations/00044` creates `share_links` with no `tenant_id`; suspension kill-switch for
share links is deferred (Wave-2) behind a JOIN-trips pattern (`db/query/share.go:714`).

---

## 1. Overview / goal

**Goal.** Introduce a real `tenants` registry plus `users.tenant_id`, keep platform admin as
the existing role `admin`(1) on tenant `'1'`, keep public `/register` landing on tenant `'1'`,
and gate the new resolver behind `MULTI_TENANT_ENABLED` (default false) so behavior is
byte-for-byte identical until the flag flips. S1 (this spec) ships migration 00102 + spec +
round-trip test only.

**Non-goals (Wave-2 deferred):** see §9 Known limitations.

---

## 2. API contract

All routes behind session auth + `ResourcePermission("tenants", "manage")`.

| Method | Path | Permission | Notes |
|---|---|---|---|
| GET | `/tenants` | `tenants:manage` | list tenants |
| POST | `/tenants` | `tenants:manage` | create tenant + admin user |
| POST | `/tenants/{id}/suspend` | `tenants:manage` | suspend + purge sessions |
| POST | `/tenants/{id}/activate` | `tenants:manage` | reactivate |

#### Create tenant — `POST /tenants`
Request:
```json
{ "name": "Acme Logistics", "admin_email": "ops@acme.in", "admin_name": "Ops Admin", "admin_password": "temp-secret" }
```
Response `201`:
```json
{
  "id": "t_acme",
  "name": "Acme Logistics",
  "slug": "acme-logistics",
  "status": "active",
  "admin_email": "ops@acme.in",
  "temp_password": "temp-secret"
}
```
The `temp_password` is shown **once**, in this response only; never stored in plaintext.
Errors: `409` slug/email collision; `403` no permission; `401` unauthenticated.

Suspend semantics: flips `tenants.status='suspended'` **and purges that tenant's sessions**;
subsequent logins flash-reject (§5.4) and bearer calls get `401` (§5.3).

---

## 3. DB contract

Canonical migration `db/migrations/00102_tenants.sql` — Up/Down pasted verbatim below
(single source of truth; the file and this section must never diverge):

```sql
-- +goose Up
CREATE TABLE tenants (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    slug       TEXT UNIQUE,
    status     TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','suspended')),
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
INSERT INTO tenants (id, name, slug) VALUES ('1', 'Default', 'default');
ALTER TABLE users ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '1';
CREATE INDEX idx_users_tenant ON users(tenant_id);
INSERT OR IGNORE INTO permissions (name, description) VALUES ('tenants:manage', 'Create and suspend tenant organizations');
INSERT OR IGNORE INTO role_permissions (role_id, permission_id) SELECT 1, id FROM permissions WHERE name = 'tenants:manage';
-- +goose Down
DELETE FROM role_permissions WHERE permission_id = (SELECT id FROM permissions WHERE name='tenants:manage');
DELETE FROM permissions WHERE name='tenants:manage';
DROP INDEX IF EXISTS idx_users_tenant;
DROP TABLE IF EXISTS tenants;
ALTER TABLE users DROP COLUMN tenant_id;
```

Rules honored: no `FOREIGN KEY REFERENCES tenants(id)` anywhere (free-form TEXT convention);
`tenant_id` defaults to `'1'` so 00065's guarantee makes Up a no-op for existing rows;
Down drops `users.tenant_id` via `ALTER TABLE ... DROP COLUMN` — safe on the bundled
`modernc.org/sqlite v1.56.0` (SQLite ≥3.35 semantics), and required for Up idempotency:
a leftover column would make any later re-up fail with `duplicate column name` and break
every down/up round-trip test crossing 00102 (repo precedent: 00092 Down drops its added
columns; `test/spec22_migration_test.go:57-58` asserts exactly that).

---

## 4. Business logic

### 4.1 TenantForUserLookup
`middleware.TenantForUserLookup` is a func type — closure over `*sql.DB`, built once in
`cmd/main.go` and handed to both middleware branches. Signature:
`func(ctx, userID) (tenantID string, status string, err error)` reading `users.tenant_id`
joined to `tenants.status`.

### 4.2 Packed cache entry
Cache shape: packed string `tenantID + "\x00" + status`, TTL 60s, stored via `cache.Cache`.
`cache.Noop` returns `ok=false` → SQL cold path every request (correct for tests/dev).

### 4.3 ErrTenantSuspended sentinel
New sentinel `ErrTenantSuspended` declared in the var-block of `internal/auth/apitoken.go`
alongside existing auth sentinels. Both middleware branches map it.

### 4.4 Web vs API rejection paths
- **Web:** `ClearSession` + `flash_error` cookie + `303 /login`. LoginPage reads **only**
  cookies (`internal/handlers/auth.go:93-125`) — a `?error=` query param is dead code; do not use it.
- **API:** the existing 401 JSON writer maps `err.Error()` through unchanged.

### 4.5 Bearer stops trusting claims.tid
Bearer branch treats `claims.tid` as advisory only; authoritative tenant/status always comes
from `TenantForUserLookup` against the DB row.

### 4.6 Login/session gate
`Login` and `CreateSessionForUser` gain a tenant-status check immediately after the existing
user-active check (`auth_service.go:43-45` and `:85-87`). Suspended → `ErrTenantSuspended`.
Register's fallback `CreateSession(Token:"")` is deleted (Spec 10 §5.2 alignment — no
non-revocable sessions).

### 4.7 Settings overlay
Per-tenant-sharded `TenantConfigReader`: same geofence reader pattern
(`config.go:114-150`) but a **sharded map keyed by tenant** — existing readers hold ONE
tenant snapshot and must not be reused. Keys (collision-free vs enumerated inventory):
`branding.{booking,trip,invoice}_prefix`, `billing.{gst_enabled,gst_rate,state_code}`.

---

## 5. Config / env

| Var | Default | Purpose | Package reading |
|---|---|---|---|
| `MULTI_TENANT_ENABLED` | `false` | Gates the real tenant resolver | `internal/config` via `getEnvBool` (AGENT_ENABLED style precedent) |

When false: resolver stays `DefaultTenantResolver` — behaviorally identical while 00065
guarantees every row reads tenant `'1'`. Flipping true requires migration 00102 applied.

---

## 6. Tests

V1 verification matrix (Prove-It gate before flag flip):
- **Two-tenant isolation:** seed tenants A/B; assert zero bleed across customers list,
  kharcha list/approve, fuel-audit reports, users list, driver-me.
- **Suspend E2E:** suspend tenant → web login gets flash-error cookie + 303 `/login`;
  API login → 401 JSON; existing bearer token → 401 with advisory-tid note; sessions purged.
- **Gate off = old behavior:** `MULTI_TENANT_ENABLED=false` → resolver returns `'1'`,
  all existing flows byte-identical.
- **Migration roundtrip:** `test/multi_tenant_migration_test.go` (shipped in S1):
  up asserts bootstrap row/column/permission; downTo(101) asserts tenants+permission gone
  (leftover `users.tenant_id` tolerated); re-up clean.

---

## 7. Phased rollout (build order) — ALL SHIPPED on `feature/multi-tenant-onboarding`

| Phase | Status | Commits |
|---|---|---|
| S1 migration 00102 + spec + roundtrip (fresh + with-data) | ✅ | `1358cc8` |
| S2 user plumbing, scoped users list, suspension gates | ✅ | `25116ef` |
| P1 resolver + edge enforcement behind `MULTI_TENANT_ENABLED` | ✅ | `1d85dd2` |
| P2 leak batch (§0.5 inventory) + dashboard cache keying | ✅ | `9e7d779` |
| P3 sharded TenantConfigReader + branding/billing overlay | ✅ | `df43bb7` |
| F1 `/tenants` UI, suspend+session purge, template-CI ×3 | ✅ | `c327a63` |
| V1 gate: live E2E a–j, race spot-check, security gate | ✅ | `11e181a` |
| Post-gate audit fixes: roleIDFromName org_admin/driver, last-position guard, real tid claims | ✅ | `a3c46ac` |
| Latent defects: Razorpay webhook tenant attribution (invoice/reference-derived), fuel-audit NULL-scan crash | ✅ | `19ed9ef` |

Live-E2E evidence matrix in V1 report: provision → login → bidirectional data
isolation → suspend kills sessions AND advisory-tid bearers → activate restores.

---

## 8. Open items / VERIFY (resolved or tracked)

- Platform admin = existing role `admin`(1) on tenant `'1'` — locked decision, no new role.
- Public `/register` keeps landing tenant `'1'` — locked decision.
- `company_settings` CHECK(id=1) singleton → overlay via `company_config` — decided (§4.7).
- Wave-2 deferrals — see §9.

---

## 8.1 Isolation guarantees (enforced, test-locked)

Every user/entity detail surface is same-tenant strict. Cross-tenant access
reads as **404** (existence undisclosed), never 403 — no enumeration signal.

| Guarantee | Enforcement | Proof |
|---|---|---|
| User list scoped | `users.tenant_id` predicate on SearchUsers/CountUsers/daterange; password_hash stripped from list rows | `TestMultiTenantUserIsolation` |
| User view/edit/update/delete same-tenant only | handler guard `ensureTenantUser` → 404 on mismatch (`handlers/users.go`) | `TestUsers_CrossTenantDetailAccessDenied` |
| Password reset cannot cross tenants (account-takeover path closed) | same guard before `ResetPassword` | subtest `password_reset_takeover_blocked` |
| Own-tenant management unaffected | guard passes when `target.TenantID == ctx tenant` | `TestUsers_SameTenantManagementStillWorks` |
| Platform globals (company_settings) locked under multi-tenant mode | `POST /settings/update` requires role `admin`(1) when `MULTI_TENANT_ENABLED=true`; org branding flows via per-tenant overlay keys instead | `TestSettings_PlatformGlobalsLockedUnderMultiTenant` |
| Login/session/bearer bound to tenant lifecycle | `AuthService.Login`+`CreateSessionForUser` tenant-active gate; resolver rejects suspended orgs; bearer `tid` advisory-only | V1 live-E2E steps h/i |
| Data-plane scoping | §0.5 inventory fixed in P2 (customers ×6, kharcha ×13, fuel-audit ×8, drivers/me, dashboard cache per-tenant key) | `multi_tenant_leaks_test.go`, inverted `TenantIsolation` |

## 9. Known limitations (Wave-2 deferred)

- `audit_logs` / `files` tenant columns → migration **00103**.
- Share-link suspension kill-switch (JOIN trips pattern `db/query/share.go:714`).
- Portal tracking intra-tenant fallback (`customer_portal.go:359-382`).
- Agent `get_open_alerts` tool (`tools.go:751`).
- PDF letterhead (`invoices.go:471`).
- KPI StorageStats.
- `live.go:266` `OR tenant_id='1'` escape hatch.
- Razorpay webhook `''`-tenant events.
- Outbox-relay latent tenant gap.
- `featureTick` single-tenant gating (`main.go:320`).
- Ops backup scripts (per-tenant restore story).
- `users.email` remains globally unique (not per-tenant).
- Logo upload moves public → authenticated later.

---

## 10. File list (create / modify)

Create (S1 scope):
- `db/migrations/00102_tenants.sql`            (§3 canonical Up/Down)
- `docs/tech-specs/24-multi-tenant-onboarding.md` (this file)
- `test/multi_tenant_migration_test.go`        (§6 round-trip)

Modify (later phases, listed for ownership clarity):
- `docs/tech-specs/00-migration-ownership-index.md` (00102 row — done in S1)
- `cmd/main.go`, `internal/middleware/api_auth.go`, `internal/middleware/middleware.go` (S2/P1)
- `internal/config/config.go` (S2 flag)
- `internal/auth/apitoken.go`, `internal/service/auth_service.go` (S2/P1)
- `db/query/*.sql` + regenerated sqlc (P2)
- `internal/static/templates/*` + template CI (F1)
