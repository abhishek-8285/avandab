#!/usr/bin/env bash
# ==============================================================================
# Avandab 24/7 ADB & Cloudflare Tunnel One-Click Deployment Script
# Domain: avandab.com
# ==============================================================================
set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
YELLOW='\033[0;33m'
NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo -e "${BLUE}==============================================================================${NC}"
echo -e "${BLUE}🚀 Starting Avandab 24/7 Automated Deployment for avandab.com${NC}"
echo -e "${BLUE}==============================================================================${NC}"

# 1. Verify ADB Connection
echo -e "\n${CYAN}[1/6] Checking connected ADB device...${NC}"
DEVICE=$(adb devices | grep -E "device$" | head -n 1 | cut -f1 || true)
if [ -z "$DEVICE" ]; then
    echo -e "${RED}❌ Error: No Android device detected via ADB.${NC}"
    echo -e "${YELLOW}Please connect your device with USB debugging enabled and retry.${NC}"
    exit 1
fi
echo -e "${GREEN}✅ Connected Device: $DEVICE${NC}"

# 2. Compile ARM64 Go Server
echo -e "\n${CYAN}[2/6] Compiling Go server for ARM64 (Android)...${NC}"
mkdir -p bin
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o bin/server_arm64 ./cmd/server/main.go
echo -e "${GREEN}✅ Compilation successful! Binary size: $(du -sh bin/server_arm64 | cut -f1)${NC}"

echo -e "\n${CYAN}[3/6] Transferring application binary and assets to Android device...${NC}"
npx @tailwindcss/cli -i src/input.css -o internal/static/css/tailwind.css --minify 2>/dev/null || true
adb shell "mkdir -p /data/local/tmp/internal/templates /data/local/tmp/internal/static"


echo "Pushing binary..."
adb push bin/server_arm64 /data/local/tmp/server
adb shell "chmod +x /data/local/tmp/server"

if [ -f "transport.db" ]; then
    echo "Pushing database (transport.db)..."
    adb push transport.db /data/local/tmp/transport.db
fi

echo "Pushing templates..."
adb push internal/templates /data/local/tmp/internal/ > /dev/null

echo "Pushing static assets..."
adb push internal/static /data/local/tmp/internal/ > /dev/null

echo -e "${GREEN}✅ File transfer completed.${NC}"

# 4. Configure ADB Port Forwarding
echo -e "\n${CYAN}[4/6] Setting up ADB port forwarding (tcp:8092)...${NC}"
adb forward tcp:8092 tcp:8092 || true
echo -e "${GREEN}✅ ADB Port forwarding ready (localhost:8092 -> TECNO:8092).${NC}"

# 5. Start Background Server with Auto-Restart Watchdog on Android Device
echo -e "\n${CYAN}[5/6] Starting Avandab server on TECNO device...${NC}"
adb shell "PID=\$(cat /data/local/tmp/mvtms_server.pid 2>/dev/null); [ -n \"\$PID\" ] && kill -9 \$PID 2>/dev/null || true; killall server 2>/dev/null || true"
cat << 'RUNEOF' > bin/start_device.sh
#!/system/bin/sh
cd /data/local/tmp
ulimit -n 65535 2>/dev/null || true

export GOMAXPROCS=8
export PORT=8092
export ENV=production
export LOG_LEVEL=info
export GODEBUG=netdns=go+1
export DATABASE_URL='file:transport.db?_journal_mode=WAL&_synchronous=NORMAL&_temp_store=MEMORY&_busy_timeout=10000&cache=shared&mode=rwc'
export APP_DOMAIN='avandab.com'
export COOKIE_SECRET="${COOKIE_SECRET:-avandab-production-secret-auth-key-2026-secure-32chars}"

# Background Auto-Restart Watchdog Loop
nohup sh -c '
while true; do
    echo "[WATCHDOG] Starting Avandab server $(date)..." >> /data/local/tmp/server_8092.log
    ./server >> /data/local/tmp/server_8092.log 2>&1
    echo "[WATCHDOG] Server stopped with exit code $?. Auto-restarting in 1s..." >> /data/local/tmp/server_8092.log
    sleep 1
done' > /dev/null 2>&1 &

echo $! > /data/local/tmp/mvtms_server.pid
RUNEOF
adb push bin/start_device.sh /data/local/tmp/start.sh > /dev/null
adb shell "chmod +x /data/local/tmp/start.sh"
adb shell "/data/local/tmp/start.sh"
sleep 2

echo -e "${YELLOW}Server Boot Output Log:${NC}"
adb shell "cat /data/local/tmp/server_8092.log 2>/dev/null | tail -n 20" || true

# 6. Direct Deployment Complete (No Cloudflare Tunnel)
echo -e "\n${BLUE}==============================================================================${NC}"
echo -e "${GREEN}🎉 Deployment Complete! Go Server running on TECNO device!${NC}"
echo -e "${BLUE}==============================================================================${NC}"
echo -e "Device Port: ${CYAN}http://192.168.1.46:8092${NC}"
echo -e "Public Web Domain: ${CYAN}http://avandab.com:8092${NC} (via Router Port Forwarding)"
echo -e "View server logs: ${YELLOW}adb shell \"cat /data/local/tmp/server_8092.log\"${NC}"
echo -e "${BLUE}==============================================================================${NC}\n"

