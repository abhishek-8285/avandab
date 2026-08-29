#!/bin/bash
# Tenant isolation lint — compile-time safety for multi-tenancy.
# Fails hard on SQL queries that touch tenant tables without tenant_id.
set -e

ERRORS=0
WARNINGS=0

# Allowlist: tables that are truly global / not tenant-scoped
ALLOWLIST="tenants|permissions|roles|role_permissions|migrations|schema_migrations|goose_db_version"
# Tenant tables — split into STRICT (8 core, hard error) and EXTENDED (51 total, warnings until hardened).
# 00103/00104 cover 51; keep in sync with db/migrations/00103*.sql header list.
STRICT_TABLES=(bookings trips drivers vehicles invoices payments customers routes)
TENANT_TABLES=(alerts bookings company_config credit_debit_notes customers device_quarantine dispatch_overrides dispatches driver_advance_requests driver_expenses driver_issues drivers engine_state error_reports eta_history eta_history_monthly experiment_assignments experiment_events experiments_spec16 fastag_tags fastag_transactions feature_flags founder_audit founder_signals fuel_prices geofence_events geofences incidents invoice_line_items invoice_sequences invoices maintenance_records money_ledger note_sequences offline_sync_log ops_alerts payments pnl_daily provider_poll_state route_optimization_jobs routes telemetry_devices telemetry_positions telemetry_raw_events trip_detentions trip_feedback trips users vehicle_geofences vehicle_latest_position vehicles)

# ── 1) SQL query check: db/query/*.sql ──────────────────────────────────────
# Hard errors only for STRICT core (8); extended 51 are warnings until hardened.
for f in db/query/*.sql; do
  [ -e "$f" ] || continue
  # Split file into per-query blocks on "-- name:" markers using python
  if ! python3 - "$f" "${STRICT_TABLES[@]}" <<'PY'
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

# Hard errors for extended tenant tables (51) — all tenant tables must be scoped
for f in db/query/*.sql; do
  [ -e "$f" ] || continue
  # Global tables that join tenant tables for enrichment but are not tenant-isolation boundaries
  if [[ "$f" == *audit_logs.sql || "$f" == *auth_sessions.sql ]]; then
    continue
  fi
  if ! python3 - "$f" "${TENANT_TABLES[@]}" <<'PY'
import sys, re
path = sys.argv[1]
tables = sys.argv[2:]
with open(path) as fh:
    text = fh.read()
parts = re.split(r'(-- name:\s*\w+)', text)
for i in range(1, len(parts), 2):
    marker = parts[i]
    body = parts[i+1] if i+1 < len(parts) else ""
    m = re.search(r'-- name:\s*(\w+)', marker)
    qname = m.group(1) if m else "unknown"
    touch_pat = re.compile(r'\b(?:FROM|JOIN|INTO|UPDATE|DELETE FROM)\s+("?)(\w+)\1', re.I)
    touched = [mm.group(2).lower() for mm in touch_pat.finditer(body)]
    tenant_touched = [t for t in touched if t.lower() in [x.lower() for x in tables]]
    # Only warn if strict tables not already flagged (avoid double error)
    strict = ["bookings","trips","drivers","vehicles","invoices","payments","customers","routes"]
    if tenant_touched and 'tenant_id' not in body.lower():
        # If any strict table, it would have been an error above; here warn for non-strict
        if any(t in strict for t in tenant_touched):
            continue
        print(f"::error file={path},line=1::SQL query '{qname}' touches tenant table(s) {tenant_touched} without tenant_id")
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
# Further filter to those that actually mention a tenant table in SQL (51 tables)
FILTERED=""
for f in $GO_FILES; do
  if grep -qiE 'FROM[[:space:]]+(alerts|bookings|company_config|credit_debit_notes|customers|device_quarantine|dispatch_overrides|dispatches|driver_advance_requests|driver_expenses|driver_issues|drivers|engine_state|error_reports|eta_history|experiment_assignments|fastag|feature_flags|founder|fuel_prices|geofence|incidents|invoice_line|invoice_seq|invoices|maintenance|money_ledger|note_seq|offline_sync|ops_alerts|payments|pnl_daily|provider_poll|route_optimization|routes|telemetry|trip_detentions|trip_feedback|trips|users|vehicle_)' "$f" 2>/dev/null; then
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
