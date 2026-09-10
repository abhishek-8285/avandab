# Spec 19 §1: Backhaul Matching Engine (B7)

- **Migration Slot:** `00068` (Reserved — No DDL required; utilizes `trips`, `trip_stops`, `bookings`, `route_locations`)
- **Owner:** Domain: `internal/trip/application` & `internal/service/backhaul_service.go` | Route: `/api/v1/backhaul/*`
- **Status:** Approved Spec (Roadmap B7)

## 1. Problem Statement & Operational Objective
Deadhead (empty return) mileage accounts for 28–38% of total fleet distance in long-haul Indian transport corridors (e.g., Delhi–Mumbai, Mumbai–Bengaluru). Backhaul Matching matches vehicles arriving at or nearing a delivery destination with unassigned bookings or available loads heading towards the vehicle's origin depot or intermediate network hubs.

## 2. Core Corridors & Matching Algorithm
1. **Trigger Points:**
   - Trip enters `ARRIVED` or `DELIVERED` status at final `trip_stops` location.
   - ETA computation indicates vehicle will arrive at destination within a 4-hour lookahead window.
2. **Spatial & Temporal Filter:**
   - Radius $R \le 50\text{ km}$ around trip destination geofence $(lat_d, lon_d)$.
   - Available pickup time window: $T_{now} \le T_{pickup} \le T_{now} + 18\text{ hours}$.
   - Corridor alignment: Haversine distance from candidate dropoff to vehicle base $\le$ direct distance from pickup to vehicle base (ensures vehicle moves homeward).
3. **Vehicle & Capacity Constraints:**
   - Vehicle payload capacity $\ge$ booking required weight.
   - Body type compatibility (e.g., reefer, flatbed, container).
   - Maintenance guard: Exclude vehicles with pending `work_orders` (`00123`) or compliance blocks (`00126`).

## 3. Lifecycle & State Machine
```
[ Trip Completing / Delivered ]
               │
               ▼
       [ Evaluate Radius ]
               │
      (Candidate Found)
               │
               ▼
    [ Match Candidate DTO ]
               │
               ▼
   [ Create Dispatch Offer ] ──(Driver Declines / Expired)──► [ Logged / Archived ]
               │
        (Driver Accepts)
               │
               ▼
     [ Booking Assigned ] ──► [ Return Trip Created ]
```

## 4. API Surface & Contracts
- `GET /api/v1/backhaul/matches`
  - Query params: `trip_id` (UUID), `radius_km` (default 50, max 150)
  - Headers: `Authorization: Bearer <token>`, `X-Tenant-ID: <tenant_id>`
  - Response: `200 OK` JSON array of candidate bookings with deadhead km, pickup distance, and freight margin.
- `POST /api/v1/backhaul/offers`
  - Request: `{"trip_id": "...", "booking_id": "...", "offered_rate": 18500}`
  - Integrates with `00109_dispatch_offers` for driver push notification and mobile acceptance.

## 5. Multi-Tenancy & Security Gate
- All candidate booking queries enforce `WHERE tenant_id = shared.TenantIDFromContext(ctx)`.
- Public/spot loadboard inter-tenant visibility is governed strictly by explicit opt-in sharing rules (`00069`).

## 6. Acceptance Criteria
1. **Zero Schema Mutations:** Operates entirely within migration `00068` reserved slot without new DDL.
2. **Performance:** Spatial bounding-box prefilter executes in $< 35\text{ms}$ on SQLite/PostgreSQL.
3. **Safety Block:** Blocked vehicles (`vehicles.status = 'blocked'` or open high-severity work orders) never appear in match results.
4. **Idempotency:** Re-querying matches for the same trip produces consistent deterministic rankings based on distance and margin.
