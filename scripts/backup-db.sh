#!/usr/bin/env bash
# Automated safe online backup for Avandab SQLite database with Cloudflare R2 sync.
set -euo pipefail

DB_PATH="${1:-/home/bhshrivastav/avandab/transport.db}"
BACKUP_DIR="${2:-/home/bhshrivastav/avandab/backups}"
TIMESTAMP="$(date -u +%Y%m%d_%H%M%SZ)"
BACKUP_FILE="${BACKUP_DIR}/transport_${TIMESTAMP}.db"
COMPRESSED_FILE="${BACKUP_FILE}.gz"

if [ ! -f "${DB_PATH}" ]; then
  echo "Error: SQLite database not found at ${DB_PATH}" >&2
  exit 1
fi

mkdir -p "${BACKUP_DIR}"

echo "==> [1/3] Creating live online SQLite snapshot..."
# Use sqlite3 online backup API to ensure zero corruption across concurrent WAL writes
sqlite3 "${DB_PATH}" ".backup '${BACKUP_FILE}'"

echo "==> [2/3] Compressing backup..."
gzip -9 "${BACKUP_FILE}"
BACKUP_SIZE="$(du -h "${COMPRESSED_FILE}" | cut -f1)"
echo "Snapshot created: ${COMPRESSED_FILE} (${BACKUP_SIZE})"

# Retain last 7 daily backups locally
echo "==> [3/3] Pruning local backups older than 7 days..."
find "${BACKUP_DIR}" -name "transport_*.db.gz" -type f -mtime +7 -delete

# Optional Cloudflare R2 upload if configured
if [ -n "${R2_BUCKET_NAME:-}" ] && [ -n "${R2_ACCOUNT_ID:-}" ] && [ -n "${R2_ACCESS_KEY_ID:-}" ] && [ -n "${R2_SECRET_ACCESS_KEY:-}" ]; then
  echo "==> Syncing backup to Cloudflare R2 (${R2_BUCKET_NAME})..."
  if command -v aws >/dev/null 2>&1; then
    AWS_ACCESS_KEY_ID="${R2_ACCESS_KEY_ID}" \
    AWS_SECRET_ACCESS_KEY="${R2_SECRET_ACCESS_KEY}" \
    aws s3 cp "${COMPRESSED_FILE}" "s3://${R2_BUCKET_NAME}/backups/$(basename "${COMPRESSED_FILE}")" \
      --endpoint-url "https://${R2_ACCOUNT_ID}.r2.cloudflarestorage.com"
    echo "R2 upload complete!"
  else
    echo "Notice: 'aws' CLI not installed on host. Local backup preserved at ${COMPRESSED_FILE}"
  fi
fi

# Sync to Google Drive (5TB Vault) if rclone remote 'gdrive' is configured
if command -v rclone >/dev/null 2>&1 && rclone listremotes 2>/dev/null | grep -q "^gdrive:"; then
  echo "==> Syncing backup to 5TB Google Drive (Avandab_Backups)..."
  rclone copy "${COMPRESSED_FILE}" "gdrive:Avandab_Backups/$(date -u +%Y)/$(date -u +%m)/"
  echo "Google Drive upload complete!"
fi

echo "==> Backup complete successfully."
