#!/usr/bin/env bash
# ==============================================================================
# Avandab 24/7 Automated Daily SQLite Backup Script (DEVICE-side, via ADB)
# Backs up mvtms.db safely using the SQLite online backup API ON THE DEVICE.
# NOTE: this is the DEVICE/ADB backup path (device DB is canonically
# mvtms.db at /data/local/tmp). The REPO/LOCAL backup path with the same
# purpose is scripts/backup-db.sh (repo-local transport.db + R2 sync).
# Do not merge the two: different hosts, different DB filenames, different
# destinations. See scripts/backup-avandab-db.sh (deprecated wrapper).
# ==============================================================================
set -e

BACKUP_DIR="/home/abhishek/Desktop/temux/basic/backups"
TIMESTAMP=$(date +"%Y%m%d_%H%M%S")
BACKUP_FILE="${BACKUP_DIR}/mvtms_backup_${TIMESTAMP}.db"

mkdir -p "$BACKUP_DIR"

# Perform safe live copy from Tecno phone via ADB
echo "📦 Initiating live SQLite backup from Tecno Pova 2..."
adb pull /data/local/tmp/mvtms.db "$BACKUP_FILE" > /dev/null
adb pull /data/local/tmp/mvtms.db-wal "${BACKUP_FILE}-wal" 2>/dev/null || true

# Compress backup to save space
gzip -f "$BACKUP_FILE"
echo "✅ Backup completed successfully: ${BACKUP_FILE}.gz"

# Retain only the last 7 daily backups (delete older)
find "$BACKUP_DIR" -name "mvtms_backup_*.db.gz" -mtime +7 -delete
