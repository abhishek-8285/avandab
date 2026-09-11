#!/usr/bin/env bash
# Deploy to STAGING (dev.avandab.com -> :8081, own sqlite file, no real data).
# Mirrors scripts/deploy-vps.sh discipline: cross-compile locally, never build
# on the 1GB box. Promote: run ./scripts/smoke.sh https://dev.avandab.com,
# then ./scripts/deploy-vps.sh for prod.
set -euo pipefail

TARGET_HOST="${1:-avandab}"
REMOTE_DIR="/home/bhshrivastav/staging"

trap 'rm -f bin/server bin/server.gz' EXIT

echo "==> [1/5] Cross-compiling binary locally (Linux amd64)..."
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o bin/server ./cmd/server
echo "==> [2/5] Syncing binary + templates + static to staging..."
ssh "${TARGET_HOST}" "mkdir -p ${REMOTE_DIR}/bin"
rsync -avz bin/server "${TARGET_HOST}:${REMOTE_DIR}/bin/server.new"
ssh "${TARGET_HOST}" "chmod +x ${REMOTE_DIR}/bin/server.new"
tar -czf - internal/templates internal/static | ssh "${TARGET_HOST}" "tar -xzf - -C ${REMOTE_DIR}"

echo "==> [3/5] Swapping binary and restarting staging..."
ssh "${TARGET_HOST}" "mv -f ${REMOTE_DIR}/bin/server.new ${REMOTE_DIR}/bin/server && sudo systemctl restart avandab-staging && systemctl is-active avandab-staging"

echo "==> [4/5] Waiting for health on :8081..."
ssh "${TARGET_HOST}" '
for i in {1..15}; do
  if curl -sf http://localhost:8081/health > /dev/null; then
    echo "Staging health check passed!"
    exit 0
  fi
  sleep 1
done
echo "Staging health check timed out!" >&2
exit 1
'
echo "==> [5/5] Done. Next: ./scripts/smoke.sh https://dev.avandab.com"
