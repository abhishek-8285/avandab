#!/usr/bin/env bash
# Automated safe online backup for the REPO-LOCAL Avandab SQLite database
# (transport.db) with optional AES-256 at-rest encryption and Cloudflare R2 sync.
# NOTE: the DEVICE/ADB backup path (device DB mvtms.db at /data/local/tmp) lives
# in ./backup_db.sh — different host, different filename, do not merge.
# scripts/backup-avandab-db.sh is a deprecated wrapper forwarding here.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DB_PATH="${1:-${REPO_ROOT}/transport.db}"
BACKUP_DIR="${2:-${REPO_ROOT}/backups}"
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

# At-rest encryption: gpg symmetric AES-256. The passphrase is fed on fd 3 so it
# never appears in the process list. Encrypted artifacts end in .gz.gpg and the
# plaintext gzip is removed; unencrypted backups warn loudly (and can be refused
# with REQUIRE_BACKUP_ENCRYPTION=1).
PASSPHRASE="${BACKUP_ENCRYPTION_PASSPHRASE:-}"
ARTIFACT="${COMPRESSED_FILE}"
if [ -n "${PASSPHRASE}" ]; then
  if [ "${#PASSPHRASE}" -lt 16 ]; then
    echo "Error: BACKUP_ENCRYPTION_PASSPHRASE must be at least 16 characters" >&2
    exit 1
  fi
  if ! command -v gpg >/dev/null 2>&1; then
    echo "Error: gpg is required for encrypted backups" >&2
    exit 1
  fi
  echo "==> [2b/3] Encrypting backup (AES-256, gpg symmetric)..."
  ENCRYPTED_FILE="${COMPRESSED_FILE}.gpg"
  gpg --batch --yes --quiet --pinentry-mode loopback --passphrase-fd 3 \
      --symmetric --cipher-algo AES256 --compress-algo none \
      --output "${ENCRYPTED_FILE}" "${COMPRESSED_FILE}" 3<<<"${PASSPHRASE}"
  rm -f "${COMPRESSED_FILE}"
  ARTIFACT="${ENCRYPTED_FILE}"
  echo "Encrypted snapshot: ${ARTIFACT} ($(du -h "${ARTIFACT}" | cut -f1))"
else
  echo "WARNING: BACKUP_ENCRYPTION_PASSPHRASE unset — writing UNENCRYPTED gzip backup." >&2
  echo "         Set BACKUP_ENCRYPTION_PASSPHRASE (>=16 chars) before any off-host copy." >&2
  if [ "${REQUIRE_BACKUP_ENCRYPTION:-0}" = "1" ]; then
    echo "Error: REQUIRE_BACKUP_ENCRYPTION=1 but BACKUP_ENCRYPTION_PASSPHRASE is unset" >&2
    exit 1
  fi
  echo "Snapshot created: ${ARTIFACT} ($(du -h "${ARTIFACT}" | cut -f1))"
fi

# Retain last 7 daily backups locally (encrypted and plaintext artifacts)
echo "==> [3/3] Pruning local backups older than 7 days..."
find "${BACKUP_DIR}" \( -name "transport_*.db.gz" -o -name "transport_*.db.gz.gpg" \) -type f -mtime +7 -delete

# Optional Cloudflare R2 upload if configured
if [ -n "${R2_BUCKET_NAME:-}" ] && [ -n "${R2_ACCOUNT_ID:-}" ] && [ -n "${R2_ACCESS_KEY_ID:-}" ] && [ -n "${R2_SECRET_ACCESS_KEY:-}" ]; then
  echo "==> Syncing backup to Cloudflare R2 (${R2_BUCKET_NAME})..."
  if command -v aws >/dev/null 2>&1; then
    AWS_ACCESS_KEY_ID="${R2_ACCESS_KEY_ID}" \
    AWS_SECRET_ACCESS_KEY="${R2_SECRET_ACCESS_KEY}" \
    aws s3 cp "${ARTIFACT}" "s3://${R2_BUCKET_NAME}/backups/$(basename "${ARTIFACT}")" \
      --endpoint-url "https://${R2_ACCOUNT_ID}.r2.cloudflarestorage.com"
    echo "R2 upload complete!"
  else
    echo "Notice: 'aws' CLI not installed on host. Local backup preserved at ${ARTIFACT}"
  fi
fi

# Sync to Google Drive (5TB Vault) if rclone remote 'gdrive' is configured.
# Residency gate: Drive stores globally, so an offshore DB copy needs explicit
# opt-in (RBI localisation posture). Without ALLOW_OFFSHORE_BACKUP=1 the local
# + R2 copies are the backup set and Drive is skipped loudly, never silently.
if command -v rclone >/dev/null 2>&1 && rclone listremotes 2>/dev/null | grep -q "^gdrive:"; then
  if [ "${ALLOW_OFFSHORE_BACKUP:-0}" = "1" ]; then
    echo "==> Syncing backup to 5TB Google Drive (Avandab_Backups)..."
    rclone copy "${ARTIFACT}" "gdrive:Avandab_Backups/$(date -u +%Y)/$(date -u +%m)/"
    echo "Google Drive upload complete!"
  else
    echo "Notice: gdrive remote present but ALLOW_OFFSHORE_BACKUP!=1 — skipping offshore copy (local + R2 retained)"
  fi
fi

echo "==> Backup complete successfully."
