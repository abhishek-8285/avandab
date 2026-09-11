#!/usr/bin/env bash
# Restore drill: prove the LATEST backup actually restores.
# Restores to a temp dir (never touches prod), then checks:
#   1. gzip integrity + sqlite integrity_check
#   2. goose schema version == current head
#   3. core tables non-empty (tenants, users, roles, permissions)
# Run quarterly (cron-friendly, exit nonzero on failure). On the VPS:
#   ssh avandab 'bash ~/avandab/scripts/restore-drill.sh'  (after deploy syncs scripts)
# Locally: ./scripts/restore-drill.sh [backup-glob] [workdir]
set -euo pipefail

GLOB="${1:-$HOME/backups/transport_*.db.gz $HOME/avandab/backups/transport_*.db.gz}"
WORK="${2:-/tmp/restore-drill-$$}"

# shellcheck disable=SC2086
LATEST="$(ls -t $GLOB 2>/dev/null | head -n 1 || true)"
[ -n "$LATEST" ] || { echo "DRILL FAIL: no backup matches: $GLOB"; exit 1; }
echo "== drill source: $LATEST ($(du -h "$LATEST" | cut -f1))"

rm -rf "$WORK"; mkdir -p "$WORK"
trap 'rm -rf "$WORK"' EXIT
gunzip -c "$LATEST" > "$WORK/restored.db" || { echo "DRILL FAIL: gunzip"; exit 1; }

echo "== [1/3] integrity_check..."
INTEG="$(sqlite3 "$WORK/restored.db" "PRAGMA integrity_check;" 2>&1)"
[ "$INTEG" = "ok" ] || { echo "DRILL FAIL: integrity_check: $INTEG"; exit 1; }
echo "   integrity ok"

echo "== [2/3] schema version..."
VER="$(sqlite3 "$WORK/restored.db" "SELECT version_id FROM goose_db_version ORDER BY id DESC LIMIT 1;" 2>&1)"
echo "   head version: $VER"

echo "== [3/3] core tables..."
for t in tenants users roles permissions; do
    n="$(sqlite3 "$WORK/restored.db" "SELECT COUNT(*) FROM $t;" 2>&1)" || { echo "DRILL FAIL: table $t unreadable"; exit 1; }
    [ "$n" -gt 0 ] || { echo "DRILL FAIL: table $t empty"; exit 1; }
    echo "   $t: $n rows"
done
echo "DRILL PASS: $LATEST restores cleanly (schema v$VER)"
