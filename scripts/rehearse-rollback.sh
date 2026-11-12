#!/usr/bin/env bash
# rehearse-rollback.sh — Rollback Drill & Timing Assertion (P5.3)
#
# Proves acceptance gate: one-command rollback completes in < 2 minutes (120s)
# and restores healthy service.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${REPO_ROOT}"

REHEARSE_PORT=18092
MAX_ALLOWABLE_SECONDS=120
TEMP_DB=$(mktemp "/tmp/rehearse_db_XXXXXX.sqlite")
SERVER_PID=""

cleanup() {
  echo "🧹 Cleaning up rehearsal test server and artifacts..."
  if [ -n "$SERVER_PID" ] && kill -0 "$SERVER_PID" 2>/dev/null; then
    kill -9 "$SERVER_PID" 2>/dev/null || true
  fi
  # Kill any stray process on rehearsal port
  fuser -k "${REHEARSE_PORT}/tcp" 2>/dev/null || true
  rm -f "$TEMP_DB" "${TEMP_DB}-wal" "${TEMP_DB}-shm"
  rm -f "bin/releases/server-v1.0.0-rehearsal" "bin/releases/server-v1.0.1-rehearsal"
  # Restore bin/server to current if needed
  if [ -f "bin/releases/current" ]; then
    CURRENT=$(cat bin/releases/current)
    if [ -f "bin/releases/server-${CURRENT}" ]; then
      ln -sfn "releases/server-${CURRENT}" bin/server
    fi
  fi
}
trap cleanup EXIT

echo "=================================================="
echo "🎭 Starting P5.3 Rollback Rehearsal Drill"
echo "   Test Port:           ${REHEARSE_PORT}"
echo "   Max Allowed Latency: ${MAX_ALLOWABLE_SECONDS}s"
echo "=================================================="

# 1. Build Rehearsal Binaries
mkdir -p bin/releases

echo "🔨 Building Version A: v1.0.0-rehearsal..."
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.Version=v1.0.0-rehearsal" -o "bin/releases/server-v1.0.0-rehearsal" ./cmd/server
chmod +x "bin/releases/server-v1.0.0-rehearsal"

echo "🔨 Building Version B: v1.0.1-rehearsal..."
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.Version=v1.0.1-rehearsal" -o "bin/releases/server-v1.0.1-rehearsal" ./cmd/server
chmod +x "bin/releases/server-v1.0.1-rehearsal"

# 2. Deploy Version A (Initial state)
ln -sfn "releases/server-v1.0.0-rehearsal" bin/server
echo "v1.0.0-rehearsal" > bin/releases/current

echo "🚀 Starting server with Version A on port ${REHEARSE_PORT}..."
PORT=${REHEARSE_PORT} \
APP_ENV=development \
LOG_LEVEL=warn \
COOKIE_SECRET="rehearsal-secret-32bytes-long-key!" \
API_SECRET="rehearsal-api-secret-key-32bytes!" \
DATABASE_URL="file:${TEMP_DB}?cache=shared&mode=rwc&_pragma=journal_mode(WAL)" \
bin/server >/dev/null 2>&1 &
SERVER_PID=$!

# Wait for Version A healthy
echo "🩺 Waiting for Version A to be healthy on :${REHEARSE_PORT}..."
HEALTHY=0
for i in {1..30}; do
  if curl -sf "http://localhost:${REHEARSE_PORT}/healthz" >/dev/null 2>&1; then
    HEALTHY=1
    break
  fi
  sleep 0.5
done

if [ "$HEALTHY" -ne 1 ]; then
  echo "❌ Version A server failed to become healthy within 15s" >&2
  exit 1
fi
echo "✅ Version A healthy and serving traffic."

# 3. Upgrade to Version B
echo "⬆️ Upgrading to Version B: v1.0.1-rehearsal..."
ln -sfn "releases/server-v1.0.1-rehearsal" bin/server
echo "v1.0.1-rehearsal" > bin/releases/current

# Restart server for Version B
kill -9 "$SERVER_PID" 2>/dev/null || true
PORT=${REHEARSE_PORT} \
APP_ENV=development \
LOG_LEVEL=warn \
COOKIE_SECRET="rehearsal-secret-32bytes-long-key!" \
API_SECRET="rehearsal-api-secret-key-32bytes!" \
DATABASE_URL="file:${TEMP_DB}?cache=shared&mode=rwc&_pragma=journal_mode(WAL)" \
bin/server >/dev/null 2>&1 &
SERVER_PID=$!

echo "🩺 Waiting for Version B to be healthy on :${REHEARSE_PORT}..."
HEALTHY=0
for i in {1..30}; do
  if curl -sf "http://localhost:${REHEARSE_PORT}/healthz" >/dev/null 2>&1; then
    HEALTHY=1
    break
  fi
  sleep 0.5
done

if [ "$HEALTHY" -ne 1 ]; then
  echo "❌ Version B server failed to become healthy within 15s" >&2
  exit 1
fi
echo "✅ Version B healthy and serving traffic."

# 4. Trigger Timed Rollback to Version A
echo "=================================================="
echo "⏱️ Starting Stopwatch: Executing Rollback to v1.0.0-rehearsal..."
echo "=================================================="

ROLLBACK_START=$(date +%s%N)

# Restart function helper for rollback: rollback swaps the binary, then restarts the server process
# Execute rollback script
PORT=${REHEARSE_PORT} ./scripts/rollback.sh v1.0.0-rehearsal

# Simulate supervisor restarting server on new active binary
kill -9 "$SERVER_PID" 2>/dev/null || true
PORT=${REHEARSE_PORT} \
APP_ENV=development \
LOG_LEVEL=warn \
COOKIE_SECRET="rehearsal-secret-32bytes-long-key!" \
API_SECRET="rehearsal-api-secret-key-32bytes!" \
DATABASE_URL="file:${TEMP_DB}?cache=shared&mode=rwc&_pragma=journal_mode(WAL)" \
bin/server >/dev/null 2>&1 &
SERVER_PID=$!

# Wait for healthy after rollback
for i in {1..30}; do
  if curl -sf "http://localhost:${REHEARSE_PORT}/healthz" >/dev/null 2>&1; then
    break
  fi
  sleep 0.5
done

ROLLBACK_END=$(date +%s%N)
ROLLBACK_DURATION_NS=$((ROLLBACK_END - ROLLBACK_START))
ROLLBACK_SECONDS=$(awk -v ns="$ROLLBACK_DURATION_NS" 'BEGIN { printf "%.2f", ns / 1000000000 }')

echo "=================================================="
echo "🏁 Rehearsal Rollback Complete in ${ROLLBACK_SECONDS} seconds!"
echo "=================================================="

# 5. Verification Assertions
# A. Timing check
if awk -v dur="$ROLLBACK_SECONDS" -v max="$MAX_ALLOWABLE_SECONDS" 'BEGIN { exit (dur > max) ? 0 : 1 }'; then
  echo "❌ Rollback took ${ROLLBACK_SECONDS}s, which exceeds the ${MAX_ALLOWABLE_SECONDS}s acceptance limit!"
  exit 1
fi
echo "✅ Rollback timing PASSED: ${ROLLBACK_SECONDS}s <= ${MAX_ALLOWABLE_SECONDS}s (< 2 minutes)"

# B. Verify active symlink points to Version A
ACTIVE_BIN_VER=$(bin/server --version 2>/dev/null || true)
if [[ "$ACTIVE_BIN_VER" != *"v1.0.0-rehearsal"* ]]; then
  echo "❌ Expected active binary to be v1.0.0-rehearsal, got: ${ACTIVE_BIN_VER}"
  exit 1
fi
echo "✅ Active binary verified: ${ACTIVE_BIN_VER}"

# C. Verify healthz responds 200 OK
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" "http://localhost:${REHEARSE_PORT}/healthz")
if [ "$HTTP_CODE" != "200" ]; then
  echo "❌ Health check returned HTTP ${HTTP_CODE}, expected 200"
  exit 1
fi
echo "✅ Health check verified: HTTP 200 OK"

echo "=================================================="
echo "🎉 P5.3 Rehearsal Drill PASSED 100%!"
echo "=================================================="
exit 0
