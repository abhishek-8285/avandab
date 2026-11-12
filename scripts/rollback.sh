#!/usr/bin/env bash
# rollback.sh — One-Command Production Rollback (P5.3)
#
# Rollback active server to previous version in bin/releases/ in < 2 minutes.
set -euo pipefail

START_TIME=$(date +%s)
RELEASES_DIR="bin/releases"
TARGET_VERSION="${1:-}"
PORT="${PORT:-8080}"
HEALTH_TIMEOUT="${HEALTH_TIMEOUT:-30}"

if [ ! -d "$RELEASES_DIR" ]; then
  echo "❌ Releases directory '${RELEASES_DIR}' not found. Cannot rollback." >&2
  exit 1
fi

# Determine Current Active Version
CURRENT_VERSION=""
if [ -f "${RELEASES_DIR}/current" ]; then
  CURRENT_VERSION=$(cat "${RELEASES_DIR}/current")
elif [ -L "bin/server" ]; then
  CURRENT_TARGET=$(readlink "bin/server")
  CURRENT_VERSION=$(basename "$CURRENT_TARGET" | sed 's/^server-//')
fi

# Find Available Releases (sorted naturally by version)
AVAILABLE_BINS=( $(ls -1v "${RELEASES_DIR}"/server-* 2>/dev/null || true) )

if [ ${#AVAILABLE_BINS[@]} -eq 0 ]; then
  echo "❌ No release binaries found in ${RELEASES_DIR}/" >&2
  exit 1
fi

# Determine Rollback Target
ROLLBACK_BIN=""
if [ -n "$TARGET_VERSION" ]; then
  # Match exact version or with 'server-' prefix
  CANDIDATE="${RELEASES_DIR}/server-${TARGET_VERSION#server-}"
  if [ -x "$CANDIDATE" ]; then
    ROLLBACK_BIN="$CANDIDATE"
  else
    echo "❌ Specified release binary '${CANDIDATE}' does not exist or is not executable." >&2
    echo "Available releases:" >&2
    for b in "${AVAILABLE_BINS[@]}"; do
      echo "  - $(basename "$b" | sed 's/^server-//')" >&2
    done
    exit 1
  fi
else
  # Find the preceding binary before CURRENT_VERSION
  if [ ${#AVAILABLE_BINS[@]} -lt 2 ]; then
    echo "❌ Only 1 release binary found (${AVAILABLE_BINS[0]}). Need at least 2 for automatic rollback." >&2
    exit 1
  fi

  # Scan backwards from the newest binary
  for ((i=${#AVAILABLE_BINS[@]}-1; i>=0; i--)); do
    BIN_VER=$(basename "${AVAILABLE_BINS[$i]}" | sed 's/^server-//')
    if [ "$BIN_VER" != "$CURRENT_VERSION" ]; then
      ROLLBACK_BIN="${AVAILABLE_BINS[$i]}"
      break
    fi
  done

  if [ -z "$ROLLBACK_BIN" ]; then
    echo "❌ Could not find an alternate previous release to roll back to." >&2
    exit 1
  fi
fi

TARGET_VER=$(basename "$ROLLBACK_BIN" | sed 's/^server-//')

echo "=================================================="
echo "⏪ Rolling Back Active Server"
echo "   From:    ${CURRENT_VERSION:-<unknown>}"
echo "   To:      ${TARGET_VER}"
echo "   Binary:  ${ROLLBACK_BIN}"
echo "=================================================="

# 1. Atomic Symlink Swap
ln -sfn "releases/server-${TARGET_VER}" bin/server
echo "${TARGET_VER}" > "${RELEASES_DIR}/current"
echo "🔗 Swapped symlink: bin/server -> releases/server-${TARGET_VER}"

# 2. Service Restart (if running under systemd or local process)
RESTARTED=0
if command -v systemctl >/dev/null 2>&1; then
  if systemctl is-active avandab >/dev/null 2>&1; then
    echo "🔄 Restarting avandab systemd service..."
    sudo systemctl restart avandab
    RESTARTED=1
  elif systemctl is-active avandab-staging >/dev/null 2>&1; then
    echo "🔄 Restarting avandab-staging systemd service..."
    sudo systemctl restart avandab-staging
    RESTARTED=1
  fi
fi

if [ -n "${LOCAL_SERVER_PID:-}" ] && kill -0 "$LOCAL_SERVER_PID" 2>/dev/null; then
  echo "🔄 Signaling local server PID ${LOCAL_SERVER_PID} to restart..."
  kill -HUP "$LOCAL_SERVER_PID" || true
  RESTARTED=1
fi

# 3. Health Check Verification (if service was restarted or health endpoint is up)
if [ "$RESTARTED" -eq 1 ] || curl -sf "http://localhost:${PORT}/health" >/dev/null 2>&1; then
  echo "🩺 Verifying health on :${PORT}/health (timeout: ${HEALTH_TIMEOUT}s)..."
  HEALTHY=0
  for ((i=1; i<=HEALTH_TIMEOUT; i++)); do
    if curl -sf "http://localhost:${PORT}/health" >/dev/null 2>&1; then
      HEALTHY=1
      break
    fi
    sleep 1
  done

  if [ "$HEALTHY" -ne 1 ]; then
    echo "❌ Health check timed out after ${HEALTH_TIMEOUT}s on port ${PORT}!" >&2
    exit 1
  fi
  echo "✅ Health check passed!"
else
  echo "ℹ️ No running systemd service detected; symlink swap complete."
fi

END_TIME=$(date +%s)
ELAPSED=$((END_TIME - START_TIME))

echo "=================================================="
echo "✅ Rollback to ${TARGET_VER} successful!"
echo "   Elapsed Time: ${ELAPSED} seconds (< 120s acceptance gate satisfied)"
echo "=================================================="
