#!/bin/bash
# Backup mvtms.db daily - keeps last 7 days
set -e
DST_DIR="/avandab/backups"
DATE=$(date +%Y-%m-%d_%H%M)
mkdir -p "$DST_DIR"
# Use host-visible proc root cp for consistency (avoids WAL lock issues)
if [ -f /proc/3710/root/data/local/tmp/mvtms.db ]; then
  cp -v /proc/3710/root/data/local/tmp/mvtms.db "$DST_DIR/mvtms.db.$DATE" 2>&1 | head -n 5
  cp -v /proc/3710/root/data/local/tmp/mvtms.db-wal "$DST_DIR/mvtms.db-wal.$DATE" 2>&1 | head -n 5 || true
  ls -lh "$DST_DIR/mvtms.db.$DATE" 2>&1 | head -n 5
  echo "backup $DATE ok size $(du -h "$DST_DIR/mvtms.db.$DATE" | cut -f1)"
  ls -t "$DST_DIR"/mvtms.db.* 2>/dev/null | tail -n +8 | xargs -r rm -v 2>&1 | head -n 10 || true
else
  echo "source not found"
  exit 1
fi
mkdir -p /data/local/tmp/backup_daily 2>&1 || mkdir -p /proc/3710/root/data/local/tmp/backup_daily 2>&1 || true
cp -v /proc/3710/root/data/local/tmp/mvtms.db /proc/3710/root/data/local/tmp/backup_daily/mvtms.db 2>&1 | head -n 5 || cp -v /proc/3710/root/data/local/tmp/mvtms.db /data/local/tmp/backup_daily/mvtms.db 2>&1 | head -n 5
echo "redundant copy done"
