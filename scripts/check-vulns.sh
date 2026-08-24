#!/bin/bash
set -e

export PATH="$(go env GOPATH)/bin:$PATH"

# Install govulncheck if not present
if ! command -v govulncheck &> /dev/null; then
  echo "Installing govulncheck..."
  go install golang.org/x/vuln/cmd/govulncheck@latest
fi

echo "Checking for known vulnerabilities in Go dependencies..."
# Fail closed on vulnerabilities reachable from this code (govulncheck exit 3).
# SECURITY_GATE_STRICT=0 downgrades to warn-only for local iteration only —
# CI and pre-commit must run strict.
set +e
govulncheck ./...
status=$?
set -e

if [ "$SECURITY_GATE_STRICT" = "0" ]; then
  echo "⚠️ SECURITY_GATE_STRICT=0: vulnerability findings downgraded to warning"
elif [ "$status" -eq 3 ]; then
  echo "❌ govulncheck found vulnerabilities reachable from this code"
  echo "Fix: upgrade the affected module (go get module@fixed && go mod tidy)"
  exit 1
elif [ "$status" -ne 0 ]; then
  echo "❌ govulncheck failed to run (exit $status)"
  exit 1
fi

echo "✅ Go vulnerability scan complete"
