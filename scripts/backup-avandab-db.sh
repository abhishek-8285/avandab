#!/bin/bash
# DEPRECATED — do not use directly. Backup entrypoint is scripts/backup-db.sh
# (online sqlite3 .backup + R2 sync). This file stays as a thin wrapper so
# existing crons referencing it keep working; it forwards to backup-db.sh.
# Canonical DB name: transport.db (repo-local default; override with $1).
set -e
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec "$SCRIPT_DIR/backup-db.sh" "$@"
