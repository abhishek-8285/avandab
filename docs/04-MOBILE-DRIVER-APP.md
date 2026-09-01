# 04. Mobile Driver Application (React Native / Expo)

> **Offline-First Driver Assistant & Live Navigation Suite**
> Designed for highway drivers in India with low-bandwidth, voice input, and multi-language support.

---

## 1. Architecture & Core Subsystems

```
 ┌─────────────────────────────────────────────────────────────────────────────┐
 │                         React Native (Expo SDK 52)                          │
 │                                                                             │
 │  ┌─────────────────────────────────┐   ┌─────────────────────────────────┐  │
 │  │ Interactive Live Map Component  │   │ Voice-Driven Kharcha Sheet      │  │
 │  │ (Leaflet.js + OpenStreetMap)    │   │ (Speech-to-Text Expense Entry)  │  │
 │  │ Origin, Dest, Route, Pulse Icon │   │ Multi-lingual Parser            │  │
 │  └─────────────────────────────────┘   └─────────────────────────────────┘  │
 │  ┌─────────────────────────────────┐   ┌─────────────────────────────────┐  │
 │  │ ePOD Capture Subsystem          │   │ Background GPS Tracker          │  │
 │  │ Signatures, Photos, 4-digit OTP │   │ expo-location, Battery Guard    │  │
 │  └─────────────────────────────────┘   └─────────────────────────────────┘  │
 │                                                                             │
 │  ┌───────────────────────────────────────────────────────────────────────┐  │
 │  │ Offline Sync Engine (services/syncEngine.ts)                          │  │
 │  │ • Reads local SQLite: offline_queue.db (queued_gps, queued_pods)     │  │
 │  │ • Auto-flushes when 4G/WiFi is restored                               │  │
 │  └───────────────────────────────────────────────────────────────────────┘  │
 └─────────────────────────────────────────────────────────────────────────────┘
```

---

## 2. Interactive Navigation Map (`mobile/src/components/LiveDriverTrackingMap.tsx`)

- **Rendering Engine**: Embedded WebView running Leaflet.js with OpenStreetMap raster tiles (zero Google Maps API billing costs).
- **Features**:
  - Live Moving Truck marker with rotating heading indicator.
  - Green Radar Pulse animation showing active GPS connection.
  - Origin pin (Green circle), Destination pin (Red circle), and Planned Route Polyline.
  - Real-time coordinates feed and Auto-recenter toggle button.

---

## 3. Offline-First Dual SQLite Architecture

The mobile app maintains two local SQLite databases on the device (`expo-sqlite`):
1. **`avandab_offline.db`**: Stores cached trips, driver profile, offline logs, and DPDP consent tokens.
2. **`offline_queue.db`**: Stores queued mutation actions executed while offline:
   - `queued_gps`: Breadcrumb location points recorded in tunnels/ghats.
   - `queued_pods`: Unsubmitted consignee signatures and proof photos.
   - `offline_expenses`: Pending fuel and toll receipts.

**Sync Engine Lifecycle**:
- Listens to `@react-native-community/netinfo` network connection state.
- When network reconnects, drains queued tables sequentially via `POST /api/v1/telemetry/sync` and `POST /api/v1/trips/{id}/epod`.

---

## 4. Multi-Lingual Regional Support (`mobile/src/i18n.ts`)

Supports instant language switching across 7 Indian regional languages:
- **English (`en`)**
- **Hindi (`hi`)** — हिंदी
- **Gujarati (`gu`)** — ગુજરાતી
- **Marathi (`mr`)** — मराठी
- **Tamil (`ta`)** — தமிழ்
- **Telugu (`te`)** — తెలుగు
- **Kannada (`kn`)** — ಕನ್ನಡ
