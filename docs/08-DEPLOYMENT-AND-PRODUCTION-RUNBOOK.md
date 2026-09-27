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

### Option C: Cross-compiled VPS Deployment (`scripts/deploy-vps.sh`) — recommended
```bash
./scripts/deploy-vps.sh <ssh-host>   # default host alias: avandab
```
The script runs `go vet` locally, cross-compiles a stripped amd64 binary
(`CGO_ENABLED=0 GOOS=linux GOARCH=amd64 -trimpath -ldflags "-s -w"`), rsyncs it
plus `internal/templates`/`internal/static`, swaps the binary under
`systemctl` (`avandab.service`), and health-checks `http://localhost:8080/health`.

**Rule: NEVER run `go build` / `go test` on the target server.** 1 GB RAM
instances OOM/swap-thrash on compile. All compilation happens locally; only
stripped binaries ship.

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

## 4b. MQTT Broker Hardening Cutover (docs/13 §5.2)

Switching the production broker from anonymous to authenticated. **Nothing here
has been executed on the VPS** — it is the reviewed procedure, and it needs
explicit sign-off because an empty password file locks out every driver *and*
the backend at once.

### Rehearse first (safe, local)

```bash
./scripts/mqtt-acl-verify.sh   # real mosquitto 2.x in Docker, all 5 checks
```

It asserts the driver's credential is delivered on its own topic, is **denied**
on a sibling driver's topic, that wrong-password and anonymous clients are
refused, and that the backend superuser still receives everything. `ACL VERIFY
GREEN` is the precondition for cutting over.

### Order of operations (each step is reversible)

1. **Export driver credentials.** `scripts/mqtt-export-credentials.sh <db>` →
   `mqtt-passwd.export` (hashes only). Empty file ⇒ stop: provision at least one
   driver first (`GET /api/v1/telemetry/mqtt-credentials` on a signed-in phone).
2. **Generate the backend superuser line** and keep the secret for step 5:
   `go run ./cmd/mqttpass avandab_backend` → prints the secret, then the line.
3. **Stage the broker config** beside the live one (do not swap yet):
   `config/mosquitto.prod.conf.example` → `<broker-dir>/mosquitto.secure.conf`,
   `config/mosquitto-acl` → `<broker-dir>/acl`, and the merged password file →
   `<broker-dir>/passwd` (`chmod 600`, owned by the broker user).
4. **Reload, don't restart** (`docker kill -s HUP <broker>` / `systemctl reload
   mosquitto`): mosquitto re-reads `password_file` and `acl_file` on SIGHUP.
5. **Point the backend at the credential** (`MQTT_USERNAME` /
   `MQTT_PASSWORD` in the service environment — `systemctl edit avandab`), then
   restart the app. Confirm in the journal: `[MQTT] Connected to broker at …`
   and no `[MQTT WARNING] Could not connect`.
6. **Verify end to end**: publish one frame from a real phone
   (`wss://avandab.com/mqtt`, topic `avandab/telemetry/drivers/{identity}/gps`)
   and confirm a new `telemetry_positions` row. A frame that does not arrive is
   the signal to roll back.

### Rollback (any step)

```bash
cp <broker-dir>/mosquitto.conf.bak <broker-dir>/mosquitto.conf   # allow_anonymous true
<reload broker>; systemctl restart avandab; unset MQTT_USERNAME MQTT_PASSWORD
```

Rollback is safe because the app still falls back to an anonymous connect when
it holds no credential, and driver GPS keeps flowing over the HTTP sync path
even while MQTT is down.

### Standing caveat

A publish denied by the ACL returns **`PUBACK RC:0`** — the phone sees success
and the fix is silently discarded. Monitor `telemetry_positions` row growth, not
client-side publish confirmations.

---

## 5. Key Environment Variables Reference

| Variable | Default | Description |
| :--- | :--- | :--- |
| `APP_ENV` | `development` | Environment mode (`development` / `production`). |
| `PORT` | `8080` | HTTP Web and API port. |
| `TELEMETRY_TCP_PORT` | `:5023` | Hardware GPS TCP socket port. |
| `MQTT_URL` | `tcp://localhost:1883` | Broker URL for the backend ingest subscriber. |
| `MQTT_USERNAME` / `MQTT_PASSWORD` | empty (unset) | Backend broker credential. Required once the broker runs `allow_anonymous false` (docs/13 §5.2); unset keeps the anonymous dev path. |
| `TELEMETRY_DEVICE_SECRET_PEPPER` | empty (unset) | HMAC-SHA256 pepper for mobile/HTTP telemetry tokens. Set before provisioning hardware — rotation invalidates stored device hashes (reprovisioning required). Non-dev startup warns when unset. |
| `RAZORPAY_KEY_ID` | empty | Razorpay API Key for payments. |
| `RAZORPAY_KEY_SECRET` | empty | Razorpay API Secret for HMAC signature verification. |
| `RAZORPAY_WEBHOOK_SECRET` | empty | Webhook HMAC secret for payments + subscription + payout webhooks. All three fail closed (503) when unset — set in prod or webhooks stay disabled. |
| `AGENT_REQUIRE_APPROVAL` | `true` | Requires admin approval for mutating AI tools. |
| `AGENT_API_KEY` | empty | OpenAI API Key for operations assistant. |
| `INTEGRATION_EWAYBILL_USE_MOCK` | `true` | Demo mode for NIC e-waybills; live needs `INTEGRATION_EWAYBILL_API_KEY` + `INTEGRATION_EWAYBILL_ENDPOINT`. |
| `INTEGRATION_GSTN_USE_MOCK` | `true` | Demo mode for GSP/GSTN; live needs `INTEGRATION_GSTN_API_KEY` (+ `USERNAME`/`PASSWORD`/`CLIENT_ID`/`CLIENT_SECRET`). |
| `INTEGRATION_FASTAG_USE_MOCK` | `true` | Demo mode for NETC FASTag; live needs `INTEGRATION_FASTAG_API_KEY` + `INTEGRATION_FASTAG_ENDPOINT`. |
| `INTEGRATION_ACCOUNTING_USE_MOCK` | `true` | Demo mode for accounting export; live needs `INTEGRATION_ACCOUNTING_ENDPOINT` + `API_KEY` + `PROVIDER`. |

> **Mock honesty:** all four providers default to mock (`*_USE_MOCK=true`). Synthetic IDs carry a `MOCK-` prefix (`EWB-MOCK-`, `MOCK-`, `JE-MOCK-`, `EXT-MOCK-`) plus `(mock)` messages and warn logs — never real provider data. Exception: GSTN e-invoice IRNs are format-locked NIC hashes (persisted on invoices), so honesty there is warn logs + `mock_qr_` payloads. Pinned by `TestMockHonesty_SyntheticIDsCarryMockPrefix`. Set the flag to `false` with live creds for production.
