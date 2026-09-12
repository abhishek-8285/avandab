#!/bin/bash
set -e

# Backend coverage: ONE honest pass over app + integration tests.
# History: this script used to profile only ./test (helpers, ~91%) while the
# real app-wide number was ~53% — a vanity gate. It also ran mobile coverage
# already owned by the Mobile CI job, and CI ran the suite 3x on top.
# Now: single -count=1 shuffled run, ratchet floor instead of a fake 80%.
echo "Checking backend coverage (single pass)..."

# shellcheck disable=SC2086
go test -timeout 20m -shuffle=on -count=1 -coverprofile=coverage.out -covermode=atomic ${COVER_PKGS:-./internal/... ./test/...}

# Apply exclusions if .covignore exists
if [ -f .covignore ]; then
  grep -v -f .covignore coverage.out > coverage.filtered.out || true
  coverage=$(go tool cover -func=coverage.filtered.out | grep total | awk '{print $3}' | sed 's/%//')
else
  coverage=$(go tool cover -func=coverage.out | grep total | awk '{print $3}' | sed 's/%//')
fi

echo "Backend coverage: ${coverage}%"

# Ratchet floor (measured 53% on 2026-09-12): fail on regression, raise the
# floor as coverage genuinely grows — never lower it without a design reason
# recorded here. Mobile coverage lives in the Mobile CI job, not here.
threshold=50
if (( $(echo "$coverage < $threshold" | bc -l) )); then
  echo "❌ Coverage ${coverage}% is below ratchet floor ${threshold}%"
  echo "Run 'go test -coverprofile=coverage.out ./internal/... ./test/... && go tool cover -html=coverage.out' to inspect"
  exit 1
fi

echo "✅ Backend coverage ${coverage}% holds the ${threshold}% ratchet floor"
