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
# Any literal TenantID assignment in Go code must derive from
# shared.TenantIDFromContext or shared.DefaultTenant (with nolint marker).
if [ -n "$LINT_BASE" ]; then
  CHANGED_GO_FILES=$(git diff --name-only --diff-filter=ACM "$LINT_BASE" -- '*.go' 2>/dev/null || true)
else
  CHANGED_GO_FILES=$(git diff --name-only --diff-filter=ACM HEAD -- '*.go' 2>/dev/null || true)
fi

if [ -n "$CHANGED_GO_FILES" ]; then
  TENANT_HITS=$(grep -nE 'TenantID(Resp)?\s*(:|=)\s*shared\.TenantID\("' $CHANGED_GO_FILES \
    | grep -v 'nolint:tenant-hardcode' \
    | grep -v '_test.go' || true)
  if [ -n "$TENANT_HITS" ]; then
    echo "❌ Hard-coded tenant literal found (Prohibition #4):"
    echo "$TENANT_HITS"
    echo "Derive tenant from shared.TenantIDFromContext(ctx) instead."
    exit 1
  fi
fi
echo "✅ No hard-coded tenant literals in changed code"

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
