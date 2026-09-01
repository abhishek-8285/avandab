# 08. Deployment, Infrastructure & Production Runbook

> **Operational Playbook for Deploying Avandab on Standalone Servers & Mobile VPS**
> Covers 1-click deployments, Cloudflare Tunnel ingress, OS socket tuning, telecom SIM networking, and OSRM routing.

---

## 1. Quick Start & Deployment Options

### Option A: Local / Linux VPS Deployment
```bash
# 1. Build the standalone binary
go build -o bin/server ./cmd/server/

# 2. Run the server
./bin/server
# Server listens on :8080 (HTTP) and :5023 (Hardware GPS TCP)
```

### Option B: 24/7 Android VPS Deployment (`deploy_avandab.sh`)
```bash
# Connect phone via USB debugging (ADB) and run:
./deploy_avandab.sh
```
The script automatically:
1. Compiles the Go binary for ARM64 (`GOOS=linux GOARCH=arm64`).
2. Pushes the binary and static assets to `/data/local/tmp/app/`.
3. Starts the server and binds the Cloudflare Tunnel to `avandab.com`.

---

## 2. OSRM Self-Hosted Routing Engine Setup

For exact turn-by-turn road distance and routing matrix in India:

```bash
# 1) Download India OSM extract (~2GB)
wget https://download.geofabrik.de/asia/india-latest.osm.pbf -O india.osm.pbf

# 2) Preprocess OSM data
docker run -t -v $(pwd):/data osrm/osrm-backend osrm-extract -p /opt/car.lua /data/india.osm.pbf
docker run -t -v $(pwd):/data osrm/osrm-backend osrm-partition /data/india.osrm
docker run -t -v $(pwd):/data osrm/osrm-backend osrm-customize /data/india.osrm

# 3) Serve on Port 5000 (<100ms per routing table query)
docker run -d -p 5000:5000 -v $(pwd):/data osrm/osrm-backend osrm-routed --algorithm mld /data/india.osrm
```

**Environment Variable**:
```text
ROUTING_PROVIDER=osrm-selfhost
OSRM_URL=http://localhost:5000
```
*Note: If OSRM is offline, the backend automatically falls back to straight-line Haversine calculations.*

---

## 3. Linux OS Socket Descriptor Tuning (`ulimit`)

For handling fleets with 5,000 to 10,000+ persistent TCP GPS connections, update `/etc/security/limits.conf`:

```text
* soft nofile 65535
* hard nofile 65535
```

Verify the setting in bash:
```bash
ulimit -n
# Expected output: 65535
```

---

## 4. Telecom Private M2M APN SIM Networking

For physical hardware GPS trackers:
1. Obtain dedicated M2M IoT SIM cards from Airtel, Jio, or Vodafone.
2. Instruct the telecom carrier to route device traffic across a **Private APN Tunnel** directly to your server's private network interface.
3. This completely shields Port `:5023` from public internet port scanners and prevents unauthorized IMEI spoofing attempts.

---

## 5. Key Environment Variables Reference

| Variable | Default | Description |
| :--- | :--- | :--- |
| `APP_ENV` | `development` | Environment mode (`development` / `production`). |
| `PORT` | `8080` | HTTP Web and API port. |
| `TELEMETRY_TCP_PORT` | `:5023` | Hardware GPS TCP socket port. |
| `TELEMETRY_DEVICE_SECRET_PEPPER` | `default-pepper` | HMAC-SHA256 secret for mobile/HTTP telemetry tokens. |
| `RAZORPAY_KEY_ID` | empty | Razorpay API Key for payments. |
| `RAZORPAY_KEY_SECRET` | empty | Razorpay API Secret for HMAC signature verification. |
| `AGENT_REQUIRE_APPROVAL` | `true` | Requires admin approval for mutating AI tools. |
| `AGENT_API_KEY` | empty | OpenAI API Key for operations assistant. |
