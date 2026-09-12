# Spec 20 §1: Customer Experience (CX) Tracking Timeline (B9)

- **Migration Slot:** `00070` (Reserved — No DDL required; utilizes `trips`, `trip_stops`, `trip_share_tokens`)
- **Owner:** Domain: `internal/trip/application` & `internal/handlers/share.go` | Route: `/share/{token}`, `/api/v1/share/{token}/timeline`
- **Status:** Approved Spec (Roadmap B9)

## 1. Problem Statement & Customer Experience Goal
Consignors, consignees, and third-party logistics coordinators require instant, zero-login visibility into shipment execution without calling dispatch operators. While `00044` introduced public share tokens and live map markers, modern CX demands an end-to-end milestone timeline (e.g., Gate-In, Loading, Dispatched, Toll Checkpoints, Reached Delivery, e-POD Verified).

## 2. Milestone Derivation Engine
The timeline is derived dynamically from canonical aggregate state and historical event logs:
1. **Milestone Taxonomy:**
   - `ORDER_CREATED`: Derived from `bookings.created_at`.
   - `VEHICLE_DISPATCHED`: Derived from `trips.started_at`.
   - `STOP_ARRIVED`: Derived from `trip_stops.reached_at` (multi-stop sequence from `00112`).
   - `IN_TRANSIT_CHECKPOINT`: Derived from geofence transit events (`00042`) or toll plaza FASTag hits (`00049`).
   - `OUT_FOR_DELIVERY`: Triggered when vehicle enters final destination geofence radius ($5\text{ km}$).
   - `DELIVERED_POD`: Derived from `trips.completed_at` + `pod_scan_value` / `pod_otp_verified_at` (`00087/00090`).
2. **PII Masking & Data Sanitization:**
   - Driver phone number masked: `+91 98*** **321`.
   - Driver earnings, carrier costs, and internal dispatch notes omitted.
   - GPS telemetry smoothed and bounded: internal battery voltage, RPM, and raw sensor diagnostics omitted from public payload.

## 3. UI & API Specifications
- **Public HTML View:** `/share/{token}`
  - Plain `setInterval` polling for step progression (verified 2026-09-12: no Datastar/HTMX on this page).
  - Color-coded badges: Green (Completed), Blue Pulse (In-Transit), Gray (Upcoming).
  - Embedded tile map displaying current vehicle location and completed path polyline.
- **JSON API Contract:** `GET /api/v1/share/{token}/timeline`
  ```json
  {
    "trip_id": "9b1deb4d-3b7d-4bad-9bdd-2b0d7b3dcb6d",
    "status": "in_transit",
    "eta": "2026-09-11T16:30:00Z",
    "milestones": [
      { "id": "m1", "title": "Dispatched from Bhiwandi Depot", "timestamp": "2026-09-11T08:15:00Z", "status": "completed" },
      { "id": "m2", "title": "Toll Plaza Crossed (Khalapur)", "timestamp": "2026-09-11T10:45:00Z", "status": "completed" },
      { "id": "m3", "title": "Arrival at Pune Hub", "timestamp": "2026-09-11T16:30:00Z", "status": "pending" }
    ]
  }
  ```

## 4. Security & Expiration Policies
- Token lifetime bounded by `trip_share_tokens.expires_at` (default 7 days post-delivery).
- Expired or revoked tokens return `HTTP 404` with no operational metadata leaked.
- Token generation is tenant-scoped; public consumers have read-only access restricted strictly to the shared trip.

## 5. Acceptance Criteria
1. Zero DB migrations added (`00070` retained in index as no-DDL specification).
2. Public timeline renders in $< 20\text{ms}$ with zero unauthenticated leaks of cross-tenant or private driver PII.
3. Mobile e-POD completion reflects on the public timeline within $\le 2\text{ seconds}$ via SSE/polling.
