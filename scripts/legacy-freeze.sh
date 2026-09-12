#!/bin/bash
# Legacy-freeze lint — the backend is mid-migration from horizontal layers to
# vertical slices. New production code MUST go in slices
# (internal/<entity>/{application,domain,infrastructure,presentation});
# the legacy dirs below are frozen: bug fixes + tests only, no new files.
# Fails hard on ADDED (not modified) non-test .go files in legacy dirs.
set -e

BASE="${LINT_BASE:-origin/master}"
LEGACY_DIRS="internal/handlers internal/service internal/repository internal/domain"

if ! git rev-parse --verify "$BASE" >/dev/null 2>&1; then
  echo "legacy-freeze: base $BASE unavailable, skipping (fetch it for ratchet mode)"
  exit 0
fi

ADDED=$(git diff --name-only --diff-filter=A "$BASE" -- $LEGACY_DIRS 2>/dev/null | grep -v "_test\.go$" || true)
UNTRACKED=$(git ls-files --others --exclude-standard -- $LEGACY_DIRS 2>/dev/null | grep "\.go$" | grep -v "_test\.go$" || true)
HITS="$(printf '%s\n%s' "$ADDED" "$UNTRACKED" | grep . || true)"
if [ -n "$HITS" ]; then
  echo "::error::New files in frozen legacy dirs — put new code in vertical slices (internal/<entity>/...), not $LEGACY_DIRS"
  echo "$HITS"
  echo ""
  echo "Legacy dirs accept bug-fix edits to existing files + new _test.go only."
  echo "Porting an entity across? Remove the legacy file in the same PR."
  exit 1
fi
echo "legacy-freeze: clean (no new non-test files in $LEGACY_DIRS)"
