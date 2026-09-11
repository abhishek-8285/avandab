#!/usr/bin/env bash
# ops-watch: 5-minute box monitor. Checks prod :8080 (/health), staging :8081
# (non-fatal), error_reports spike in the last hour, disk and memory.
# Alerts via Telegram Bot API when OPS_TELEGRAM_BOT_TOKEN + OPS_TELEGRAM_CHAT_ID
# are set in ~/.ops-watch.env (0600); otherwise logs to ~/ops-watch.log.
# Transition-only alerts with 1h repeat cooldown (state in /tmp/ops-watch/).
# Install: */5 * * * * /home/bhshrivastav/avandab/scripts/ops-watch.sh
set -uo pipefail

DB="/home/bhshrivastav/avandab/transport.db"
ENV_FILE="/home/bhshrivastav/.ops-watch.env"
STATE_DIR="/tmp/ops-watch"
LOG="/home/bhshrivastav/ops-watch.log"
mkdir -p "$STATE_DIR"
[ -f "$ENV_FILE" ] && set -a && . "$ENV_FILE" && set +a

alert() { # key, message — sends on transition to fail, repeats hourly
    local key="$1" msg="$2" now marker
    now=$(date +%s); marker="$STATE_DIR/$key"
    if [ -f "$marker" ]; then
        [ $(( now - $(cat "$marker") )) -lt 3600 ] && return 0
    fi
    echo "$now" > "$marker"
    echo "$(date -u +%FT%TZ) ALERT [$key] $msg" >> "$LOG"
    logger -t ops-watch "[$key] $msg"
    if [ -n "${OPS_TELEGRAM_BOT_TOKEN:-}" ] && [ -n "${OPS_TELEGRAM_CHAT_ID:-}" ]; then
        curl -s --max-time 10 -X POST "https://api.telegram.org/bot${OPS_TELEGRAM_BOT_TOKEN}/sendMessage" \
            --data-urlencode "chat_id=${OPS_TELEGRAM_CHAT_ID}" \
            --data-urlencode "text=🚨 avandab [$key] $msg" -o /dev/null || true
    fi
}

clear_alert() { rm -f "$STATE_DIR/$1"; }

# 1. prod health (fatal)
if curl -sf --max-time 10 http://127.0.0.1:8080/health >/dev/null; then
    clear_alert prod_down
else
    alert prod_down "prod :8080/health failing"
fi

# 2. staging health (non-fatal, info only)
if curl -sf --max-time 10 http://127.0.0.1:8081/health >/dev/null; then
    clear_alert staging_down
else
    alert staging_down "staging :8081/health failing (non-fatal)"
fi

# 3. error spike: occurrences logged in the last hour
if [ -f "$DB" ]; then
    ERR1H="$(sqlite3 "$DB" "SELECT COALESCE(SUM(occurrences),0) FROM error_reports WHERE last_seen >= datetime('now','-1 hour');" 2>/dev/null || echo 0)"
    if [ "${ERR1H:-0}" -gt 25 ]; then
        TOP="$(sqlite3 "$DB" "SELECT substr(message,1,120) FROM error_reports WHERE last_seen >= datetime('now','-1 hour') ORDER BY occurrences DESC LIMIT 1;" 2>/dev/null || true)"
        alert error_spike "${ERR1H} error occurrences in the last hour. Top: ${TOP}"
    else
        clear_alert error_spike
    fi
fi

# 4. disk + memory
DISK_USED="$(df / --output=pcent | tail -n 1 | tr -dc '0-9')"
[ "${DISK_USED:-0}" -gt 85 ] && alert disk_full "root disk ${DISK_USED}% used" || clear_alert disk_full
MEM_AVAIL="$(free -m | awk '/^Mem:/ {print $7}')"
[ "${MEM_AVAIL:-9999}" -lt 120 ] && alert mem_low "only ${MEM_AVAIL}MB RAM available" || clear_alert mem_low

# 5. units alive
for u in avandab avandab-staging cloudflared mosquitto; do
    if systemctl is-active --quiet "$u"; then clear_alert "unit_$u"; else alert "unit_$u" "systemd unit $u not active"; fi
done
