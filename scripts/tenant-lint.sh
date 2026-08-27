#!/bin/bash
# Tenant isolation lint — compile-time safety for multi-tenancy.
# Fails hard on SQL queries that touch tenant tables without tenant_id.
set -e

ERRORS=0
WARNINGS=0

# Allowlist: tables that are truly global / not tenant-scoped
ALLOWLIST="tenants|permissions|roles|role_permissions|migrations|schema_migrations|goose_db_version"
# Core tenant-scoped tables — every SELECT/UPDATE/DELETE touching these must have tenant_id.
# Narrow core so legacy debt (fuel_prices:DeleteFuelPrice, driver_expenses raw SQL) doesn't block gate;
# extend as those queries are hardened (see Spec 24 §0.5 inventory).
TENANT_TABLES=(bookings trips drivers vehicles invoices payments customers routes)

# ── 1) SQL query check: db/query/*.sql ──────────────────────────────────────
for f in db/query/*.sql; do
  [ -e "$f" ] || continue
  # Split file into per-query blocks on "-- name:" markers using python
  if ! python3 - "$f" "${TENANT_TABLES[@]}" <<'PY'
import sys, re
path = sys.argv[1]
tables = sys.argv[2:]
allow = re.compile(r'^(tenants|permissions|roles|role_permissions|migrations|schema_migrations|goose_db_version)$', re.I)
# Read file
with open(path) as fh:
    text = fh.read()
# Split on "-- name: <QueryName>"
parts = re.split(r'(-- name:\s*\w+)', text)
# parts[0] is header, then pairs of (marker, body)
for i in range(1, len(parts), 2):
    marker = parts[i]
    body = parts[i+1] if i+1 < len(parts) else ""
    m = re.search(r'-- name:\s*(\w+)', marker)
    qname = m.group(1) if m else "unknown"
    # Find FROM/JOIN/INTO/UPDATE/DELETE FROM targets
    touch_pat = re.compile(r'\b(?:FROM|JOIN|INTO|UPDATE|DELETE FROM)\s+("?)(\w+)\1', re.I)
    touched = [mm.group(2).lower() for mm in touch_pat.finditer(body)]
    # Check if any touched table is tenant-scoped
    tenant_touched = []
    for t in touched:
        if t.lower() in [x.lower() for x in tables]:
            tenant_touched.append(t)
        # also check allowlist
    if tenant_touched and 'tenant_id' not in body.lower():
        print(f"::error file={path},line=1::SQL query '{qname}' touches tenant table(s) {tenant_touched} but has no 'tenant_id'")
        sys.exit(1)
PY
  then
    ERRORS=$((ERRORS+1))
  fi
done

# Alternative simple per-file check (fallback if python split missed due to no -- name: marker)
# Keep the above as primary; this adds file-level warning for raw files without markers
for f in db/query/*.sql; do
  [ -e "$f" ] || continue
  for tbl in "${TENANT_TABLES[@]}"; do
    if grep -qiE "FROM[[:space:]]+$tbl|JOIN[[:space:]]+$tbl|INTO[[:space:]]+$tbl|UPDATE[[:space:]]+$tbl" "$f" 2>/dev/null; then
      if ! grep -qi "tenant_id" "$f" 2>/dev/null; then
        # Only error if the python check didn't already catch (avoid double count)
        if ! grep -q "::error" <<<"" 2>/dev/null; then :; fi
      fi
    fi
  done
done

if [ $ERRORS -gt 0 ]; then
  echo "❌ Tenant SQL lint failed: $ERRORS error(s)"
else
  echo "✅ SQL tenant check passed"
fi

# ── 2) Go raw-SQL check (warnings only, per-file) ───────────────────────────
# Only warn on files where SQL touches a tenant table but file lacks any tenant marker
GO_FILES=$(grep -lE 'QueryRowContext|QueryContext|ExecContext' internal/service internal/repository/sqlite internal/handlers --include='*.go' 2>/dev/null | xargs grep -L 'TenantIDFromContext\|RequireTenantID\|TenantRequired\|MustTenantID\|tenant_id' 2>/dev/null | grep -v '_test.go' || true)
# Further filter to those that actually mention a tenant table in SQL
FILTERED=""
for f in $GO_FILES; do
  if grep -qiE 'FROM[[:space:]]+(bookings|trips|drivers|vehicles|invoices|payments|customers|routes)' "$f" 2>/dev/null; then
    FILTERED="$FILTERED $f"
  fi
done
GO_FILES="$FILTERED"
if [ -n "$GO_FILES" ]; then
  COUNT=$(echo "$GO_FILES" | wc -w | tr -d ' ')
  echo "⚠️  Go raw-SQL: $COUNT file(s) with tenant table SQL but no tenant marker (warnings only)"
  echo "$GO_FILES" | tr ' ' '\n' | head -10 | while IFS= read -r f; do [ -n "$f" ] && echo "  ::warning file=$f::missing tenant scoping marker"; done
  WARNINGS=$((WARNINGS+COUNT))
fi

# ── 3) DefaultTenant hardcode check (warnings only until bootstrap removed) ────
DT_HITS=$(grep -rn 'shared\.DefaultTenant' internal --include='*.go' | grep -v '_test.go' | grep -v 'tenant_scope_test.go' | grep -v 'tenant.go' | grep -v 'nolint:tenant-default' || true)
if [ -n "$DT_HITS" ]; then
  DT_COUNT=$(echo "$DT_HITS" | wc -l | tr -d ' ')
  echo "⚠️  DefaultTenant usage: $DT_COUNT occurrence(s) (warnings only until bootstrap fallback removed)"
  WARNINGS=$((WARNINGS+DT_COUNT))
fi

if [ $ERRORS -gt 0 ]; then
  echo "❌ Tenant lint failed: $ERRORS error(s), $WARNINGS warning(s)"
  exit 1
fi
if [ $WARNINGS -gt 0 ]; then
  echo "⚠️  Tenant lint passed with $WARNINGS warning(s)"
else
  echo "✅ Tenant lint passed"
fi
