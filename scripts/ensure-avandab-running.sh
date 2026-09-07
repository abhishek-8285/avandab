#!/bin/sh
# Avandab Always-On Ensure Script - for TECNO-LE7 device
# Ensures server 8092, dnsmasq, cloudflared, watchdog stay running
# Safe to run repeatedly (idempotent)
set -e

echo "[$(date)] ensure: checking..."

# 1. Check server 8092 - via proc tcp6 hex 1F9C = 8092
if ! cat /proc/net/tcp6 2>/dev/null | grep -q 1F9C; then
  echo "  server 8092 down -> restarting"
  cd /data/local/tmp
  export PORT=8092
  export APP_ENV=development
  export LOG_LEVEL=info
  export COOKIE_SECRET='dev-secret-32bytes-for-cookie-signing!'
  export DATABASE_URL='file:/data/local/tmp/mvtms.db?cache=shared&mode=rwc&_journal_mode=WAL&_busy_timeout=10000'
  export STATIC_DIR=/data/local/tmp/internal/static
  export TEMPLATES_DIR=/data/local/tmp/internal/templates
  nohup ./server >> server_8092.log 2>&1 &
  echo $! > mvtms_server.pid
  echo "  started server pid $(cat mvtms_server.pid)"
else
  echo "  server 8092 UP pid $(cat /data/local/tmp/mvtms_server.pid 2>/dev/null || echo unknown)"
fi

# 2. Check dnsmasq
if ! pgrep -f "dnsmasq.*--server=8.8.8.8" >/dev/null 2>&1; then
  echo "  dnsmasq down -> restarting"
  dnsmasq -k -x /data/local/tmp/dnsmasq.pid --no-resolv --listen-address=127.0.0.1 --listen-address=::1 --server=8.8.8.8 --server=1.1.1.1 >> /data/local/tmp/dnsmasq.log 2>&1 &
  sleep 1
  renice -n 19 -p $(cat /data/local/tmp/dnsmasq.pid) 2>/dev/null || true
  taskset -cp 0 $(cat /data/local/tmp/dnsmasq.pid) 2>/dev/null || true
  echo "  dnsmasq pid $(cat /data/local/tmp/dnsmasq.pid)"
else
  echo "  dnsmasq UP pid $(cat /data/local/tmp/dnsmasq.pid 2>/dev/null)"
fi

# 3. Check cloudflared
if ! pgrep -f "cloudflared.*80c07818" >/dev/null 2>&1; then
  echo "  cloudflared down -> restarting"
  cd /data/local/tmp
  nohup ./cloudflared --edge-ip-version 4 --config /data/local/tmp/config.yml tunnel run 80c07818-cd81-4dfd-9970-e53d70acd334 >> cloudflared.log 2>&1 &
  echo $! > cloudflared.pid
  echo "  cloudflared pid $(cat cloudflared.pid)"
else
  echo "  cloudflared UP pid $(pgrep -f "cloudflared.*80c07818")"
fi

# 4. Check watchdog
if ! pgrep -f watchdog.sh >/dev/null 2>&1; then
  echo "  watchdog down -> restarting"
  nohup /data/local/tmp/watchdog.sh >> /data/local/tmp/watchdog.log 2>&1 &
  echo "  watchdog pid $!"
else
  echo "  watchdog UP"
fi

# 5. Health check
sleep 1
if curl -s http://127.0.0.1:8092/health 2>&1 | grep -q '"status":"UP"'; then
  echo "  health UP"
else
  echo "  health FAIL - check server_8092.log"
fi

# 6. Throttle opencode if running as root scan
if ps -o pid,cmd -p 4453 2>&1 | grep -q "opencode"; then
  # ensure nice 19 and cpuset 0,1 and oom 500
  renice -n 19 -p 4453 2>/dev/null || true
  taskset -cp 0,1 4453 2>/dev/null || true
  echo 500 > /proc/4453/oom_score_adj 2>/dev/null || true
  echo "  opencode throttled"
fi

echo "[$(date)] ensure: done"
