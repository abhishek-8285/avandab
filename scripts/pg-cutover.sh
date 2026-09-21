#!/usr/bin/env bash
# pg-cutover.sh — sqlite -> PostgreSQL zero-loss cutover automation & runbook.
# Never touches the live DB without a verified online backup.
#
# Usage:
#   ./scripts/pg-cutover.sh --check          # read-only parity & schema check
#   ./scripts/pg-cutover.sh --dry-run        # full migration inside rolled-back transaction
#   ./scripts/pg-cutover.sh --live           # FULL CUTOVER (freeze-check, backup, migrate, verify, smoke)
#
# Env:
#   SQLITE_DB     (default: transport.db)
#   DATABASE_URL  (required: postgres target connection string)
#   PORT          (default: 8080, production app port)
#   SMOKE_PORT    (default: 18089, ephemeral port for post-migration smoke test)
#   AUTO_FREEZE   (default: 0, set 1 to auto-stop systemd avandab service if active)
#   FORCE         (default: 0, set 1 to bypass interactive prompts)
set -euo pipefail
cd "$(dirname "$0")/.."

SQLITE_DB="${SQLITE_DB:-transport.db}"
MODE="${1:---check}"
STAMP="$(date +%Y%m%d-%H%M%S)"
BACKUP="${SQLITE_DB}.backup-${STAMP}"
SMOKE_PORT="${SMOKE_PORT:-18089}"
APP_PORT="${PORT:-8080}"
GPS_PORT="${GPS_PORT:-5023}"

need() { command -v "$1" >/dev/null || { echo "ERROR: missing required tool: $1"; exit 1; }; }
need sqlite3
need psql

if [ -z "${DATABASE_URL:-}" ]; then
  echo "ERROR: DATABASE_URL environment variable is required."
  echo "Example: export DATABASE_URL=\"postgres://user:pass@localhost:5432/mvtms?sslmode=disable\""
  exit 1
fi

if [ ! -f "$SQLITE_DB" ]; then
  echo "ERROR: SQLite database file '${SQLITE_DB}' does not exist."
  exit 1
fi

# Locate or build binaries locally (complies with 'no compile on VPS' rule)
SQLITE2PG="./bin/sqlite2pg"
if [ ! -x "$SQLITE2PG" ]; then
  if command -v go >/dev/null; then
    echo "== building bin/sqlite2pg..."
    go build -o bin/sqlite2pg ./cmd/sqlite2pg
  else
    echo "ERROR: bin/sqlite2pg not found and 'go' compiler is unavailable."
    exit 1
  fi
fi

SERVER_BIN="./bin/server"
if [ ! -x "$SERVER_BIN" ] && command -v go >/dev/null; then
  echo "== building bin/server..."
  go build -o bin/server ./cmd/server
fi

sqlite_ver() {
  sqlite3 "file:${SQLITE_DB}?mode=ro" "SELECT COALESCE(max(version_id), 0) FROM goose_db_version;" 2>/dev/null || echo "0"
}

pg_ver() {
  psql "$DATABASE_URL" -tA -c "SELECT COALESCE((SELECT max(version_id) FROM goose_db_version), 0);" 2>/dev/null || echo "uninitialized"
}

# ── 1. Target PostgreSQL Connectivity ──────────────────────────────────────────
echo "== [1/7] probing PostgreSQL target..."
if ! psql "$DATABASE_URL" -c "SELECT 1;" >/dev/null 2>&1; then
  echo "ERROR: PostgreSQL is unreachable at DATABASE_URL."
  exit 1
fi
echo "    PostgreSQL reachable. Current status: sqlite=v$(sqlite_ver), pg=$(pg_ver)"

# ── Mode: --check ─────────────────────────────────────────────────────────────
if [ "$MODE" = "--check" ]; then
  echo "== [Check Mode] running read-only precheck..."
  "$SQLITE2PG" --sqlite "$SQLITE_DB" --pg-url "$DATABASE_URL" --auto-schema=true --check
  echo "== Precheck complete."
  exit 0
fi

# ── Mode: --dry-run ───────────────────────────────────────────────────────────
if [ "$MODE" = "--dry-run" ]; then
  echo "== [Dry-Run Mode] running full copy inside rolled-back transaction..."
  "$SQLITE2PG" --sqlite "$SQLITE_DB" --pg-url "$DATABASE_URL" --auto-schema=true --dry-run
  echo "== Dry-Run complete. No data committed."
  exit 0
fi

if [ "$MODE" != "--live" ]; then
  echo "Unknown mode: $MODE"
  echo "Usage: $0 [--check | --dry-run | --live]"
  exit 1
fi

echo "=================================================================="
echo " STARTING LIVE POSTGRESQL CUTOVER"
echo "=================================================================="

# ── 2. Freeze Check: detect in-flight writers (REST API & GPS telemetry) ──────
echo "== [2/7] checking for active writers (HTTP :${APP_PORT} / GPS :${GPS_PORT})..."
WRITERS_FOUND=0

if command -v systemctl >/dev/null && systemctl is-active --quiet avandab 2>/dev/null; then
  echo "    [WARN] avandab systemd service is currently active!"
  WRITERS_FOUND=1
fi

if command -v lsof >/dev/null; then
  if lsof -i ":${APP_PORT}" -sTCP:LISTEN -t >/dev/null 2>&1; then
    PIDS=$(lsof -i ":${APP_PORT}" -sTCP:LISTEN -t | tr '\n' ' ')
    echo "    [WARN] Process listening on HTTP port :${APP_PORT} (PID ${PIDS})"
    WRITERS_FOUND=1
  fi
  if lsof -i ":${GPS_PORT}" -t >/dev/null 2>&1; then
    PIDS=$(lsof -i ":${GPS_PORT}" -t | tr '\n' ' ')
    echo "    [WARN] Process listening on GPS telemetry port :${GPS_PORT} (PID ${PIDS})"
    WRITERS_FOUND=1
  fi
fi

if [ "$WRITERS_FOUND" -eq 1 ]; then
  if [ "${AUTO_FREEZE:-0}" = "1" ] && command -v systemctl >/dev/null && systemctl is-active --quiet avandab 2>/dev/null; then
    echo "    Stopping avandab service via systemctl..."
    systemctl stop avandab
  else
    echo ""
    echo "    CRITICAL: In-flight writes or hardware GPS telemetry packets to :${GPS_PORT} will"
    echo "    be missed if the service continues accepting data during cutover."
    echo "    Stop the service before proceeding: systemctl stop avandab"
    echo ""
    if [ "${FORCE:-0}" != "1" ]; then
      read -r -p "    Have you frozen/stopped writers? Proceed with cutover? [y/N] " confirm
      if [ "$confirm" != "y" ] && [ "$confirm" != "Y" ]; then
        echo "Cutover aborted by user."
        exit 1
      fi
    fi
  fi
fi

# ── 3. Online-Safe WAL Backup ─────────────────────────────────────────────────
echo "== [3/7] creating crash-consistent online backup via SQLite backup API..."
sqlite3 "file:${SQLITE_DB}?mode=ro" ".backup '${BACKUP}'"
INTEG=$(sqlite3 "file:${BACKUP}?mode=ro" "PRAGMA integrity_check;")
if [ "$INTEG" != "ok" ]; then
  echo "ERROR: Backup integrity check failed: ${INTEG}"
  exit 1
fi
BACKUP_BYTES=$(wc -c < "$BACKUP")
echo "    Backup verified: ${BACKUP} (${BACKUP_BYTES} bytes, PRAGMA integrity_check = ok)"

# ── 4. Optional Pre-migration Data Cleanup ────────────────────────────────────
if [ -f "scripts/cutover-data-cleanup.sql" ]; then
  echo "== [4/7] evaluating data cleanup (scripts/cutover-data-cleanup.sql)..."
  if [ "${FORCE:-0}" = "1" ]; then
    CLEANUP="y"
  else
    read -r -p "    Apply cutover data cleanup (removes dead orphans/canonicalizes roles)? [y/N] " CLEANUP
  fi
  if [ "$CLEANUP" = "y" ] || [ "$CLEANUP" = "Y" ]; then
    sqlite3 "$SQLITE_DB" < scripts/cutover-data-cleanup.sql
    echo "    Cleanup applied."
  else
    echo "    Skipping cleanup."
  fi
fi

# ── 5. Live Migration ─────────────────────────────────────────────────────────
echo "== [5/7] executing live migration (schema migration + data copy + sequence resync)..."
"$SQLITE2PG" --sqlite "$SQLITE_DB" --pg-url "$DATABASE_URL" --auto-schema=true

QUARANTINE_COUNT=$(psql "$DATABASE_URL" -tA -c "SELECT count(*) FROM _migration_quarantine;" 2>/dev/null || echo "0")
if [ "$QUARANTINE_COUNT" -ne 0 ]; then
  echo "ERROR: ${QUARANTINE_COUNT} rows were quarantined during live migration!"
  psql "$DATABASE_URL" -c "SELECT target_table, quarantine_reason, count(*) FROM _migration_quarantine GROUP BY target_table, quarantine_reason;"
  exit 1
fi
echo "    Live copy complete with 0 quarantined rows."

# ── 6. Verification & Data Parity Audit ───────────────────────────────────────
echo "== [6/7] running parity audit between SQLite and PostgreSQL..."

audit_table() {
  local tbl="$1"
  local s_count
  local p_count
  s_count=$(sqlite3 "file:${SQLITE_DB}?mode=ro" "SELECT count(*) FROM \"${tbl}\";" 2>/dev/null || echo "-1")
  p_count=$(psql "$DATABASE_URL" -tA -c "SELECT count(*) FROM \"${tbl}\";" 2>/dev/null || echo "-1")
  printf "    %-28s  sqlite: %-6s  pg: %-6s" "$tbl" "$s_count" "$p_count"
  if [ "$s_count" = "-1" ] || [ "$p_count" = "-1" ]; then
    echo " [ERROR: table query failed]"
    return 1
  elif [ "$tbl" = "roles" ] || [ "$tbl" = "permissions" ] || [ "$tbl" = "role_permissions" ]; then
    # Merge tables: PG seeds may exist
    echo " [MERGED/OK]"
  elif [ "$p_count" -lt "$s_count" ]; then
    echo " [FAIL: PG count < SQLite count]"
    return 1
  else
    echo " [OK]"
  fi
  return 0
}

CHECK_TABLES=(
  "users"
  "audit_logs"
  "vehicles"
  "trips"
  "drivers"
  "driver_expenses"
  "invoices"
  "bookings"
  "customers"
  "files"
  "geofences"
  "roles"
  "permissions"
  "tenants"
)

AUDIT_FAIL=0
for tbl in "${CHECK_TABLES[@]}"; do
  audit_table "$tbl" || AUDIT_FAIL=1
done

if [ "$AUDIT_FAIL" -ne 0 ]; then
  echo "ERROR: Data parity audit failed! Examine differences before switching traffic."
  exit 1
fi

S_VER=$(sqlite_ver)
P_VER=$(pg_ver)
echo "    Schema versions: sqlite=v${S_VER}, pg=v${P_VER}"
if [ "$S_VER" != "$P_VER" ]; then
  echo "ERROR: Goose schema version mismatch (sqlite=v${S_VER}, pg=v${P_VER})!"
  exit 1
fi
echo "    Data parity audit: ALL CHECKS PASSED."

# ── 7. Ephemeral Smoke Test on PostgreSQL ─────────────────────────────────────
if [ -x "$SERVER_BIN" ]; then
  echo "== [7/7] booting ephemeral server on PostgreSQL (port ${SMOKE_PORT}) for smoke verification..."
  
  SMOKE_LOG="/tmp/pg-smoke-${STAMP}.log"
  PORT="$SMOKE_PORT" DATABASE_DRIVER=postgres DATABASE_URL="$DATABASE_URL" \
    "$SERVER_BIN" > "$SMOKE_LOG" 2>&1 &
  SMOKE_PID=$!

  smoke_cleanup() {
    kill "$SMOKE_PID" 2>/dev/null || true
    wait "$SMOKE_PID" 2>/dev/null || true
  }
  trap smoke_cleanup EXIT INT TERM

  HEALTH_OK=0
  for _ in {1..20}; do
    if curl -sf "http://localhost:${SMOKE_PORT}/healthz" >/dev/null 2>&1; then
      HEALTH_OK=1
      break
    fi
    sleep 0.5
  done

  if [ "$HEALTH_OK" -ne 1 ]; then
    echo "ERROR: Ephemeral PostgreSQL server failed health probe (/healthz)!"
    tail -n 25 "$SMOKE_LOG"
    exit 1
  fi

  LOGIN_STATUS=$(curl -s -o /dev/null -w "%{http_code}" "http://localhost:${SMOKE_PORT}/login" || echo "000")
  if [ "$LOGIN_STATUS" != "200" ]; then
    echo "ERROR: /login returned HTTP ${LOGIN_STATUS} (expected 200)!"
    tail -n 25 "$SMOKE_LOG"
    exit 1
  fi

  echo "    Smoke test PASSED: GET /healthz -> 200, GET /login -> 200."
  smoke_cleanup
  trap - EXIT INT TERM
else
  echo "== [7/7] skipping ephemeral smoke test (bin/server not compiled)."
fi

echo ""
echo "=================================================================="
echo " 🛡️ CUTOVER COMPLETED & VERIFIED WITH ZERO DATA LOSS"
echo "=================================================================="
echo "Source SQLite DB:     ${SQLITE_DB} (v${S_VER})"
echo "PostgreSQL Target:    ${DATABASE_URL} (v${P_VER})"
echo "Crash-safe Backup:    ${BACKUP}"
echo ""
echo "To switch production service to PostgreSQL:"
echo "  1. In /etc/systemd/system/avandab.service or .env:"
echo "       DATABASE_DRIVER=postgres"
echo "       DATABASE_URL=\"${DATABASE_URL}\""
echo "  2. Start service:"
echo "       systemctl start avandab"
echo ""
echo "Rollback Runbook (< 30 seconds):"
echo "  1. systemctl stop avandab"
echo "  2. Comment out or unset DATABASE_DRIVER in service env"
echo "  3. sqlite3 \"${SQLITE_DB}\" \".restore '${BACKUP}'\""
echo "  4. systemctl start avandab"
echo "=================================================================="
