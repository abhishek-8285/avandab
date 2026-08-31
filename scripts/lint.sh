#!/bin/bash
set -e

export PATH="$(go env GOPATH)/bin:$PATH"

# Install golangci-lint if not present (v2 config requires v2 binary)
if ! command -v golangci-lint &> /dev/null; then
  echo "Installing golangci-lint..."
  go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
fi
# Ensure v2 is installed even if v1 is present (config version: "2")
if golangci-lint version 2>&1 | grep -q "version 1\."; then
  echo "Upgrading golangci-lint v1 -> v2 for config version 2..."
  go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
fi

echo "Running golangci-lint..."
# LINT_BASE (optional): git rev to diff against — gates only new code.
# Used by pre-commit so legacy debt doesn't block commits; full run when unset.
ARGS=(run --timeout=8m)
if [ -n "$LINT_BASE" ]; then
  ARGS+=("--new-from-rev=$LINT_BASE")
fi
golangci-lint "${ARGS[@]}" ./...

echo "✅ golangci-lint passed"
