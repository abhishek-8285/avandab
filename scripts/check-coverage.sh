#!/bin/bash
set -euo pipefail

# Backend coverage: ONE honest pass over app + integration tests.
# History: this script used to profile only ./test (helpers, ~91%) while the
# real app-wide number was ~53% — a vanity gate. It also ran mobile coverage
# already owned by the Mobile CI job, and CI ran the suite 3x on top.
# Now: single -count=1 shuffled run, ratchet floor instead of a fake 80%.
echo "Checking backend coverage (single pass)..."

# shellcheck disable=SC2086
go test -timeout 20m -shuffle=on -count=1 -coverprofile=coverage.out -covermode=atomic ${COVER_PKGS:-./internal/... ./test/...}

# Apply exclusions if .covignore exists
profile=coverage.out
if [ -f .covignore ]; then
  grep -v -f .covignore coverage.out > coverage.filtered.out || true
  profile=coverage.filtered.out
fi
# awk (not grep|awk: pipefail would kill the script when grep finds no
# "total", and grep's exit code otherwise masks a failed `go tool cover`).
coverage=$(go tool cover -func="$profile" | awk '/^total:/{gsub(/%/,"",$3); print $3}')
if ! [[ "${coverage:-}" =~ ^[0-9]+(\.[0-9]+)?$ ]]; then
  echo "❌ Could not parse total coverage from $profile (got '${coverage:-<empty>}')"
  exit 1
fi

echo "Backend coverage: ${coverage}%"

# Ratchet floor (measured 53% on 2026-09-12): fail on regression, raise the
# floor as coverage genuinely grows — never lower it without a design reason
# recorded here. Mobile coverage lives in the Mobile CI job, not here.
threshold=50
# awk float compare: bc is not guaranteed on CI images, and an empty/garbled
# $coverage made the old `(( $(...|bc -l) ))` test silently pass the gate.
if awk -v cov="$coverage" -v floor="$threshold" 'BEGIN { exit (cov < floor) ? 0 : 1 }'; then
  echo "❌ Coverage ${coverage}% is below ratchet floor ${threshold}%"
  echo "Run 'go test -coverprofile=coverage.out ./internal/... ./test/... && go tool cover -html=coverage.out' to inspect"
  exit 1
fi

echo "✅ Backend coverage ${coverage}% holds the ${threshold}% ratchet floor"
