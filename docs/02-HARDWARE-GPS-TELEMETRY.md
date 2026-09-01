# 02. Hardware GPS Telematics, Decoders & Ingestion Pipeline

> **Complete Specification for Fleet GPS Hardware Communication, Parsing & Scale Constraints**
> Handles 5,000+ real-world vehicles on single-node hardware without Redis, Kafka, or external message brokers.

---

## 1. Multi-Channel Ingestion Architecture

```
 ┌─────────────────────────────────────────────────────────────────────────────┐
 │                         INGESTION INGRESS CHANNELS                          │
 │                                                                             │
 │ [ Chinese GPS (GT06/Concox) ]    ──► TCP Socket (:5023) ──┐                 │
 │ [ Indian Govt GPS (AIS-140) ]    ──► TCP Socket (:5023) ──┼──► [ Async Queue ]
 │ [ Driver Mobile App (HTTPS) ]    ──► REST API (:8080)   ──┤    (Cap 10,000) │
 │ [ Fleet IoT Gateways (MQTT) ]    ──► MQTT (:1883)       ──┘         │       │
 │                                                                     │       │
 │                                                                     ▼       │
 │                                                         ┌─────────────────┐ │
 │                                                         │   Worker Pool   │ │
 │                                                         │ (4 Goroutines)  │ │
 │                                                         └────────┬────────┘ │
 │                                                                  │          │
 │                                                                  ▼          │
 │                                                       ┌───────────────────┐ │
 │                                                       │ Deduplication,    │ │
 │                                                       │ Outbox Event, &   │ │
 │                                                       │ SQLite Persistence│ │
 │                                                       └───────────────────┘ │
 └─────────────────────────────────────────────────────────────────────────────┘
```

---

## 2. Hardware Protocols & Decoders

### A. GT06 / Concox Binary Protocol (`internal/telemetry/providers/gt06.go`)
- **Transport**: Persistent TCP stream on port `:5023`.
- **Packet Structure**:
  - `0x01` **Login Handshake**: Decodes 8-byte BCD encoded IMEI (15 digits), registers active socket session, and transmits mandatory 10-byte ACK (`0x78 0x78 0x05 0x01 [serial] [crc] 0x0D 0x0A`).
  - `0x12` / `0x22` **Location Fix**: Decodes latitude/longitude (with hemisphere bitmasks), ground speed (km/h), course/heading, satellite count, and GPS validity.
  - `0x13` **Heartbeat / Status**: Decodes ignition wire status (ON/OFF), battery percentage (0-100%), and GSM signal strength (0-4 bars).
  - `0x16` **Alarm / SOS**: Decodes SOS panic button triggers and sends instant ACK to silence device alarm.
- **Checksum**: ITU-16 CRC calculator (`CalculateGT06CRC`).

### B. Indian AIS-140 Standard (`internal/telemetry/providers/ais140.go`)
- **Transport**: ASCII strings over TCP on port `:5023`.
- **Packet Structure**: `$PVT,IMEI,Date(DDMMYYYY),Time(HHMMSS),Lat,LatDir,Lng,LngDir,Speed,Heading,Satellites,Ignition,Emergency*Checksum`.
- **Coordinate Transformation**: Converts raw NMEA `DDMM.MMMM` format into signed standard decimal degrees.

### C. Mobile App & REST Ingest (`internal/telemetry/http_ingest.go`)
- **Endpoint**: `POST /api/v1/telemetry/devices/{imei}/gps` and `POST /api/v1/telemetry/sync`.
- **Security**: HMAC-SHA256 device token verification (`X-Device-Token`) using `TELEMETRY_DEVICE_SECRET_PEPPER`.

---

## 3. Data Integrity, Deduplication & Edge Constraints

| Mechanism | Code Location | Operational Protection |
| :--- | :--- | :--- |
| **Async Ring-Buffer** | `async_queue.go` | Non-blocking 10,000 capacity queue. Enqueues frames in `< 0.1ms` for immediate tracker ACKs. |
| **Out-of-Order Guard** | `ingest.go` | When a tracker reconnects after a 2-3 hour network drop and dumps 1,000 offline points, the live map (`vehicle_latest_position`) only accepts the newest timestamp. Older breadcrumbs go cleanly to history without causing the truck on the map to jump backwards. |
| **Parked Dedup Guard** | `ingest.go` | When speed is 0 km/h and distance moved is < 50 meters within 10 minutes, duplicate stationary rows are dropped, saving ~70% database storage. |
| **Quarantine Isolation**| `quarantine.go` | Unknown IMEIs are isolated into `telemetry_quarantine` table with raw payloads, preventing unauthorized devices from affecting live operations. |
| **Socket Timeout** | `tcp_ingest.go` | Strict 5-minute read deadlines disconnect idle sockets to prevent Slowloris resource exhaustion. |

---

## 4. Hardware Scaling Thresholds & Scale Triggers

| Fleet Size | Expected Load | Memory Footprint | Database Strategy | Action Trigger |
| :--- | :--- | :--- | :--- | :--- |
| **1 – 1,000 Trucks** | 100 req/sec | ~150 MB RAM | SQLite WAL mode (Single file) | None (100% smooth) |
| **1,000 – 5,000 Trucks** | 500 req/sec | ~450 MB RAM | SQLite WAL + In-Memory Queue | Tune `ulimit -n 65535` |
| **5,000 – 25,000+ Trucks**| 2,500 req/sec | ~1.8 GB RAM | TimescaleDB / PostgreSQL migration | Migrate telemetry history |

---

## 5. Live Tracking UI Marker Badges (`internal/templates/tracking.html`)

When viewing trucks on the web command center (`/tracking`), marker tooltips dynamically render:
- **Provider Badge**: `📡 AIS-140 GPS`, `📡 Hardware Tracker`, `📡 Teltonika OBD`, or `📱 Mobile App`.
- **Battery Health**: Live device battery percentage with `⚠` low power indicator.
- **GSM Signal**: Signal strength bars (`●●●●`, `●●●○`, `●○○○`).
- **Engine Status**: `Running (Speed km/h)` vs `PARKED` vs `No GPS fix`.
