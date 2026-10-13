#!/usr/bin/env bash
# Export MQTT broker password-file lines from driver_mqtt_credentials.
#
# The broker is hardened (docs/13 §5.2: allow_anonymous false + acl_file). Its
# password file therefore needs one line per credential, and the DB is the
# source of truth. Only the mosquitto PBKDF2-SHA512 hash is ever stored or
# exported — the plaintext was handed to the device once and is not recoverable
# by design.
#
# Usage:
#   scripts/mqtt-export-credentials.sh [transport.db]        # writes ./mqtt-passwd.export
#   scripts/mqtt-export-credentials.sh --append              # append to /mosquitto/config/passwd (in the broker container)
#
# After a copy that changes, reload the broker: it re-reads password_file and
# acl_file on SIGHUP. Drivers whose secret was rotated keep publishing under
# the old secret until that reload.
set -euo pipefail

DB="${1:-transport.db}"
OUT="${2:-mqtt-passwd.export}"

if ! command -v sqlite3 >/dev/null 2>&1; then
  echo "sqlite3 is required" >&2
  exit 1
fi

# A missing table means migrations have not run; say so instead of exporting an
# empty file, which would lock every driver out of publishing.
if ! sqlite3 "$DB" "SELECT 1 FROM driver_mqtt_credentials LIMIT 1" >/dev/null 2>&1; then
  echo "driver_mqtt_credentials is missing from $DB — run migrations first (goose to 00168)" >&2
  exit 1
fi

sqlite3 "$DB" \
  "SELECT username || ':' || password_hash FROM driver_mqtt_credentials ORDER BY username;" \
  > "$OUT"

COUNT=$(wc -l < "$OUT")
echo "wrote $COUNT credential line(s) to $OUT"
if [ "$COUNT" -eq 0 ]; then
  echo "warning: no credentials yet — a broker switched to allow_anonymous false with an empty" >&2
  echo "         password file refuses every client, including the backend superuser." >&2
fi
