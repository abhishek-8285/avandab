#!/usr/bin/env bash
# Deploy Avandab to VPS using local cross-compilation.
# NEVER run 'go build' on the VPS (prevents OOM/swap exhaustion on 1GB instances).
set -euo pipefail

TARGET_HOST="${1:-avandab}"
REMOTE_DIR="/home/bhshrivastav/avandab"

# Trap to ensure local scratch files are cleaned up on exit
trap 'rm -f bin/server bin/server.gz' EXIT

echo "==> [1/6] Running pre-deploy checks locally..."
go vet ./...

echo "==> [2/6] Cross-compiling binary locally (Linux amd64)..."
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o bin/server ./cmd/server
echo "==> [3/6] Transferring binary to ${TARGET_HOST} with rsync..."
rsync -avzP bin/server "${TARGET_HOST}:${REMOTE_DIR}/bin/server.new"
ssh "${TARGET_HOST}" "chmod +x ${REMOTE_DIR}/bin/server.new"

echo "==> [4/6] Syncing templates and static assets to ${TARGET_HOST}..."
tar -czf - internal/templates internal/static | ssh "${TARGET_HOST}" "tar -xzf - -C ${REMOTE_DIR}"

echo "==> [5/6] Performing binary swap and service restart..."
ssh "${TARGET_HOST}" "sudo systemctl stop avandab && mv -f ${REMOTE_DIR}/bin/server.new ${REMOTE_DIR}/bin/server && sudo systemctl start avandab && systemctl is-active avandab"

echo "==> [6/6] Verifying health status..."
ssh "${TARGET_HOST}" '
for i in {1..15}; do
  if curl -sf http://localhost:8080/health > /dev/null; then
    echo "Health check passed!"
    exit 0
  fi
  sleep 1
done
echo "Health check timed out!" >&2
exit 1
'

echo "==> Deploy complete! Service active and health check passed."
