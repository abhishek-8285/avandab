#!/usr/bin/env bash
# pg-cutover.sh — sqlite -> Postgres cutover runbook (executable checklist).
# Never touches the live DB without a backup. Dry-run first:
#
#   ./scripts/pg-cutover.sh --check          # read-only go/no-go
#   ./scripts/pg-cutover.sh                  # FULL CUTOVER (backs up, migrates)
#
# Env: SQLITE_DB (default transport.db), DATABASE_URL (postgres target),
#      PORT (default 8080), PG_CONTRAINT... none. Requires: sqlite3, go, psql.
set -euo pipefail
cd "$(dirname "$0")/.."

SQLITE_DB="${SQLITE_DB:-transport.db}"
MODE="${1:---check}"
STAMP="$(date +%Y%m%d-%H%M%S)"
BACKUP="transport.db.backup-${STAMP}"

need() { command -v "$1" >/dev/null || { echo "missing: $1"; exit 1; }; }
need sqlite3; need go; need psql
[ -n "${DATABASE_URL:-}" ] || { echo "DATABASE_URL required"; exit 1; }

sqlite_ver() { sqlite3 "file:${SQLITE_DB}?mode=ro" "SELECT max(version_id) FROM goose_db_version;"; }
pg_ver() { psql "$DATABASE_URL" -tA -c "SELECT max(version_id) FROM goose_db_version;"; }

echo "== sqlite: ${SQLITE_DB} (v$(sqlite_ver))  pg: v$(pg_ver)"

if [ "$MODE" = "--check" ]; then
  go run ./cmd/sqlite2pg --sqlite "$SQLITE_DB" --pg-url "$DATABASE_URL" --check
  echo "CHECK done. MISMATCH above? boot the server once on sqlite first (auto-migrates), then re-run."
  exit 0
fi

echo "== 0. freeze writes (one binary serves API+TCP+MQTT)"
echo "    systemctl stop avandab  # and confirm: curl /login -> 000"
echo "== 1. backup ${SQLITE_DB} -> ${BACKUP} (backup API: cp on WAL-mode files can copy malformed)"
sqlite3 "file:${SQLITE_DB}?mode=ro" ".backup '${BACKUP}'"
sqlite3 "file:${BACKUP}?mode=ro" "PRAGMA integrity_check;"
ls -la "$BACKUP"

echo "== 2. boot server on sqlite (auto-migrate to latest)"
PORT="${PORT:-8080}" DATABASE_URL="file:${SQLITE_DB}?mode=rwc&cache=shared&_foreign_keys=on&_journal_mode=WAL" \
  timeout 60 go run ./cmd/server > "/tmp/pg-cutover-sqlite-${STAMP}.log" 2>&1 &
SRV=$!
sleep 40
kill "$SRV" 2>/dev/null || true
echo "sqlite now v$(sqlite_ver) (want $(pg_ver))"

echo "== 3. optional data cleanup (answers 'yes' to apply)"
read -r -p "Apply scripts/cutover-data-cleanup.sql? [y/N] " ans
if [ "$ans" = "y" ] || [ "$ans" = "Y" ]; then
  sqlite3 "$SQLITE_DB" < scripts/cutover-data-cleanup.sql
fi

echo "== 4. dry-run migration"
go run ./cmd/sqlite2pg --sqlite "$SQLITE_DB" --pg-url "$DATABASE_URL" --dry-run 2>&1 | tail -8

echo "== 5. LIVE migration"
read -r -p "Proceed with LIVE copy? [y/N] " live
if [ "$live" = "y" ] || [ "$live" = "Y" ]; then
  go run ./cmd/sqlite2pg --sqlite "$SQLITE_DB" --pg-url "$DATABASE_URL" 2>&1 | tail -8
fi

echo "== 6. boot server on postgres and smoke-test login"
PORT="${PORT:-8080}" DATABASE_DRIVER=postgres DATABASE_URL="$DATABASE_URL" \
  timeout 60 go run ./cmd/server > "/tmp/pg-cutover-pg-${STAMP}.log" 2>&1 &
SRV=$!
sleep 40
curl -s -o /dev/null -w "login:%{http_code}\n" "http://localhost:${PORT:-8080}/login" || true
kill "$SRV" 2>/dev/null || true

echo "== done. Rollback = stop server, restore ${BACKUP}, boot without DATABASE_DRIVER."
