# 01. Platform Architecture & Technology Stack

> **Avandab / MVTMS (Multi-Vehicle Transport Management System)**
> Modern, high-throughput, multi-tenant fleet management SaaS platform built in pure Go.

---

## 1. High-Level Architectural Design

The platform uses a clean, vertical-slice Domain-Driven Design (DDD) with a synchronous event bus, transactional outbox pattern, and reactive server-sent events (SSE).

```
 ┌─────────────────────────────────────────────────────────────────────────────┐
 │                           cmd/server/main.go                                │
 │                                                                             │
 │  ┌───────────────────────┐  ┌───────────────────────┐  ┌─────────────────┐  │
 │  │ Domain Slices (DDD)   │  │ Core Services         │  │ Ingestion Hub   │  │
 │  │ • booking/    trip/   │  │ • User / Auth / RBAC  │  │ • TCP :5023     │  │
 │  │ • invoice/    payment/│  │ • Fuel Audit / PnL    │  │ • HTTP /api/v1  │  │
 │  │ • entitlement/settle/ │  │ • Maintenance / Alerts│  │ • MQTT :1883    │  │
 │  └───────────┬───────────┘  └───────────┬───────────┘  └────────┬────────┘  │
 │              │                          │                       │           │
 │              ▼                          ▼                       ▼           │
 │  ┌───────────────────────────────────────────────────────────────────────┐  │
 │  │ Transactional Outbox Relay & In-Memory Event Bus (internal/events/)    │  │
 │  └───────────────────────────────────┬───────────────────────────────────┘  │
 │                                      │                                      │
 │                                      ▼                                      │
 │  ┌───────────────────────────────────────────────────────────────────────┐  │
 │  │ SQLite Database (modernc.org/sqlite — Pure Go, WAL Mode, Zero CGO)    │  │
 │  └───────────────────────────────────────────────────────────────────────┘  │
 └─────────────────────────────────────────────────────────────────────────────┘
```

---

## 2. Technology Stack

| Layer | Technology | Key Details & Advantages |
| :--- | :--- | :--- |
| **Backend Core** | Go 1.26 | Compiled single binary with zero external runtime dependencies. |
| **HTTP Routing** | `go-chi/chi/v5` | Lightweight, idiomatic HTTP router with strict middleware layering. |
| **Database** | SQLite (pure Go) | `modernc.org/sqlite` with WAL mode (`PRAGMA journal_mode=WAL`), zero CGO. |
| **Migrations** | `goose/v3` | Embedded Go migrations (`db/migrations/*.sql`) executing idempotently. |
| **Web UI / Templates** | HTML5 + Datastar + HTMX | Server-rendered reactive hypermedia with zero JavaScript build pipeline. |
| **Styling** | Tailwind CSS | Modern utility classes with light/dark aura modes. |
| **Live Map Frontend** | Leaflet.js + OSM | OpenStreetMap tiles with custom SVG truck markers and cluster groups. |
| **Event Bus** | In-Memory Bus + Outbox | Thread-safe in-memory publish/subscribe with transactional durability. |
| **Hardware Ingest** | Pure Go TCP Socket | Direct binary packet listener on port `:5023` for physical GPS trackers. |
| **AI Assistant** | OpenAI API + Tool RL | Autonomous operations agent with human-in-the-loop approval gate. |

---

## 3. Core Directory Layout

```
├── cmd/
│   ├── server/             # Main application entry point (HTTP, TCP :5023, MQTT)
│   ├── agent/              # AI assistant CLI & operations worker
│   └── rag/                # Local codebase documentation search & embedder
├── db/
│   ├── migrations/         # Canonical Goose SQL migrations (00001 - 00117)
│   └── query/              # Sqlc query definitions
├── docs/                   # Unified, accurate platform documentation
├── internal/
│   ├── booking/            # Booking aggregate & lifecycle management
│   ├── trip/               # Trip dispatch, tracking, and proof-of-delivery
│   ├── telemetry/          # High-throughput GPS ingestion, TCP gateway, & queue
│   ├── geofence/           # Geofence polygon math & auto-transition worker
│   ├── invoice/            # GST compliant invoice generator & PDF export
│   ├── payment/            # Razorpay gateway & driver payout accounting
│   ├── realtime/           # Server-Sent Events (SSE) live map stream hub
│   ├── agent/              # AI orchestrator & reinforcement learning loop
│   └── shared/             # Tenancy, Money, Outbox, and Clock utilities
└── mobile/                 # React Native / Expo driver mobile app
```
