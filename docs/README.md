# Avandab Fleet Platform Documentation

> **Official Canonical Documentation Suite for Avandab / MVTMS Fleet SaaS**
> 100% unified, zero-duplication, and synchronized with the pure Go backend and React Native mobile app.

---

## 🗺️ Master Documentation Index

```
 ┌─────────────────────────────────────────────────────────────────────────────┐
 │                             CORE ARCHITECTURE                               │
 │  01. Architecture & Tech Stack (docs/01-ARCHITECTURE-AND-STACK.md)          │
 │  02. Hardware GPS & Telemetry  (docs/02-HARDWARE-GPS-TELEMETRY.md)          │
 │  03. Trip & Geofence Engine   (docs/03-TRIP-AND-GEOFENCE-ENGINE.md)         │
 │  04. Mobile Driver Application (docs/04-MOBILE-DRIVER-APP.md)               │
 ├─────────────────────────────────────────────────────────────────────────────┤
 │                            FINANCE & OPERATIONS                             │
 │  05. Billing, GST & Settle     (docs/05-BILLING-GST-AND-SETTLEMENTS.md)      │
 │  06. Auth, RBAC & Multi-Tenant (docs/06-AUTH-RBAC-AND-MULTI-TENANCY.md)      │
 │  07. AI Operations Assistant   (docs/07-AI-OPERATIONS-ASSISTANT.md)          │
 │  08. Deployment & Runbook      (docs/08-DEPLOYMENT-AND-PRODUCTION-RUNBOOK.md)│
 ├─────────────────────────────────────────────────────────────────────────────┤
 │                               REFERENCE                                     │
 │  • Migration Registry (1-117)  (docs/tech-specs/00-migration-ownership-index.md)
 │  • REST API Specification      (openapi.yaml)                                │
 └─────────────────────────────────────────────────────────────────────────────┘
```

---

## 📂 Master Numbered Guide

| Document | Purpose & Scope |
| :--- | :--- |
| **[`01-ARCHITECTURE-AND-STACK.md`](01-ARCHITECTURE-AND-STACK.md)** | Core Go backend, Chi router, pure Go SQLite WAL database, and zero-Docker runtime. |
| **[`02-HARDWARE-GPS-TELEMETRY.md`](02-HARDWARE-GPS-TELEMETRY.md)** | Port `:5023` TCP listener, GT06 & AIS-140 binary/ASCII decoders, 10k async ring-buffer, deduplication & scale constraints. |
| **[`03-TRIP-AND-GEOFENCE-ENGINE.md`](03-TRIP-AND-GEOFENCE-ENGINE.md)** | Booking intake, Trips state machine, polygon Ray-Casting math, geofence auto-transitions, detention billing, and ePOD OTP. |
| **[`04-MOBILE-DRIVER-APP.md`](04-MOBILE-DRIVER-APP.md)** | Expo React Native app, Leaflet OSM live navigation map, voice kharcha, and dual SQLite offline sync queues. |
| **[`05-BILLING-GST-AND-SETTLEMENTS.md`](05-BILLING-GST-AND-SETTLEMENTS.md)** | GST CGST/SGST/IGST tax engine, PDF invoices with UPI QR codes, double-entry driver balance ledger, and Razorpay gateway. |
| **[`06-AUTH-RBAC-AND-MULTI-TENANCY.md`](06-AUTH-RBAC-AND-MULTI-TENANCY.md)** | Tenant context isolation, Casbin 6-role RBAC matrix, session cookies, and API bearer token security. |
| **[`07-AI-OPERATIONS-ASSISTANT.md`](07-AI-OPERATIONS-ASSISTANT.md)** | Multi-agent orchestrator (Booking, Payments, Kharcha, Ops, Support), online RL loop, and admin safety approval gate. |
| **[`08-DEPLOYMENT-AND-PRODUCTION-RUNBOOK.md`](08-DEPLOYMENT-AND-PRODUCTION-RUNBOOK.md)** | 1-click Android VPS deploy (`deploy_avandab.sh`), Cloudflare Tunnel, OSRM routing setup, Linux `ulimit -n 65535`, and private M2M APN SIMs. |
| **[`tech-specs/00-migration-ownership-index.md`](tech-specs/00-migration-ownership-index.md)** | Goose SQL database migration numbering index (00001 to 00117). |
