#!/bin/bash
# ==============================================================================
# Security Gate — MUST run on every change (agents, humans, CI alike).
# Policy owner: AGENTS.md "Security Gate (mandatory on every change)".
# Ratchet: set LINT_BASE=<git-rev> to gate only code newer than that rev
# (pre-commit uses HEAD so legacy lint debt doesn't block commits).
# SECURITY_GATE_STRICT=0 downgrades dependency-vuln failures to warnings —
# local iteration only, never in CI.
# ==============================================================================
set -e

echo "=== Security Scanner Suite ==="

./scripts/lint.sh
./scripts/check-vulns.sh
./scripts/check-npm-audit.sh

# ── Hard-coded multi-tenancy scan (Prohibition #4) ──────────────────────────
# Fail on literal tenant assignments in Go code: TenantID:"1", TenantID="1",
# TenantID: shared.TenantID("1"), shared.DefaultTenant WITHOUT nolint marker.
# The sanctioned seam (shared.DefaultTenant WITH //nolint:tenant-default plus
# justification, e.g. bootstrap/global-scope) stays legal.
if [ -n "$LINT_BASE" ]; then
  CHANGED_GO_FILES=$(git diff --name-only --diff-filter=ACM "$LINT_BASE" -- '*.go' 2>/dev/null || true)
else
  CHANGED_GO_FILES=$(git diff --name-only --diff-filter=ACM HEAD -- '*.go' 2>/dev/null || true)
fi

if [ -n "$CHANGED_GO_FILES" ]; then
  # internal/shared/tenant.go is the sanctioned HOME of the bootstrap seam
  # (DefaultTenant's const definition) — tenant-lint.sh excludes it too.
  TENANT_SCAN_FILES=$(echo "$CHANGED_GO_FILES" | grep -v '^internal/shared/tenant\.go$' || true)
  TENANT_HITS=$(grep -nE 'TenantID(Resp)?\s*(:|=)\s*("1"|shared\.TenantID\("1"|shared\.DefaultTenant)' $TENANT_SCAN_FILES \
    | grep -v 'nolint:tenant-default' \
    | grep -v 'nolint:tenant-hardcode' \
    | grep -v '_test.go' || true)
  if [ -n "$TENANT_HITS" ]; then
    echo "❌ Hard-coded tenant literal found (Prohibition #4):"
    echo "$TENANT_HITS"
    echo "Derive tenant from shared.TenantIDFromContext(ctx) instead,"
    echo "or mark the sanctioned bootstrap seam with //nolint:tenant-default + justification."
    exit 1
  fi
fi
echo "✅ No hard-coded tenant literals in changed code"

# ── Request-path panic guard (Prohibition: no 5xx from a bad session) ────────
# MustTenantID panics when the tenant is absent. On an HTTP handler that turns
# a bad/expired session into a 500 via Recoverer instead of a 401. New usage
# inside internal/handlers/ is blocked; the pre-existing call sites are
# grandfathered by the LINT_BASE ratchet (this scan looks at ADDED lines only).
if [ -n "$LINT_BASE" ]; then
  NEW_MUST_TENANT=$(git diff "$LINT_BASE" -- 'internal/handlers/*.go' 'internal/*/presentation/**/*.go' 2>/dev/null \
    | grep -E '^\+' \
    | grep -vE '^\+\+\+' \
    | grep -E 'MustTenantID\(' \
    | grep -v '_test.go' || true)
  if [ -n "$NEW_MUST_TENANT" ]; then
    echo "❌ New MustTenantID() on a request path (panics -> 500 for a bad session):"
    echo "$NEW_MUST_TENANT" | head -10
    echo "Use shared.TenantRequired(ctx) and answer 401 (see handlers/share.go requireTenant)."
    exit 1
  fi
  echo "✅ No new MustTenantID() on request paths"
fi

# ── Tenant isolation lint (compile-time safety) ──────────────────────────────
./scripts/tenant-lint.sh

# ── Secret-pattern scan on changed files ─────────────────────────────────────
if [ -n "$LINT_BASE" ]; then
  SECRET_DIFF=$(git diff "$LINT_BASE" -- '*.go' '*.sh' '*.yml' '*.yaml' '*.env*' 2>/dev/null \
    | grep -E '^\+' \
    | grep -vE '^\+\+\+' \
    | grep -Ei '(BEGIN (RSA |EC |OPENSSH )?PRIVATE KEY|AKIA[0-9A-Z]{16}|(password|secret|api_key|apikey|token)\s*[:=]\s*["'"'"'][A-Za-z0-9+/_-]{16,}["'"'"'])' \
    | grep -vEi '(change-?me|example|placeholder|dev-secret|xxx|dummy|test)' || true)
else
  SECRET_DIFF=""
fi
if [ -n "$SECRET_DIFF" ]; then
  echo "❌ Possible secret committed in changed lines:"
  echo "$SECRET_DIFF" | head -10
  echo "Move secrets to environment variables (.env is git-ignored)."
  exit 1
fi
echo "✅ No secret patterns in changed lines"

echo "✅ All security checks passed"
