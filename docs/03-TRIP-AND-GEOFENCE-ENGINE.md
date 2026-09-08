# 03. Booking, Trip Execution & Geofence Engine

> **Complete Specification for Dispatch, Trips State Machine, Geofencing & Proof of Delivery**
> Fully autonomous lifecycle from booking intake to geofence detection, detention billing, and ePOD completion.

---

## 1. Booking Lifecycle & Invariants

```text
Draft → Pending → Confirmed → Completed
            ↓         ↓
            └────► Cancelled
```

(Cancel is allowed from Pending or Confirmed — `Cancel` in `booking_aggregate.go` rejects only Completed. Confirmed bookings are live: only they can be completed.)

- **Creation**: Requires valid `CustomerID`, `RouteID`, `VehicleType`, `Passengers >= 1`, and `Price >= 0`.
- **Confirmation**: Only `Pending` bookings can be confirmed; moves to `Confirmed`.
- **Immutability**: Once `Completed` or `Cancelled`, no further edits are permitted.
- **Audit**: Every state change is written to the immutable `audit_logs` table.

---

## 2. Trip Execution State Machine

Trips follow a strict, domain-guarded state machine (`internal/trip/domain/aggregate/trip_aggregate.go:26-35`):

```text
DRAFT → SCHEDULED → ASSIGNED → STARTED → REACHED_PICKUP → IN_TRANSIT → DELIVERED → COMPLETED
  │          │          │          │            │              │             │
  └──────────┴──────────┴──────────┴────────────┴──────────────┴─────────────┴──► CANCELLED
```

(Mobile mirrors these via `BACKEND_TO_MOBILE` in `mobile/src/domain/trip/tripMachine.ts`; clients send commands, never raw statuses.)

### State Progression Rules:
1. **DRAFT $\to$ SCHEDULED**: Scheduling a draft advances the trip. Driver/vehicle overlap conflict checks run on the separate assign paths (`assign_driver.go`, `assign_vehicle.go`).
2. **SCHEDULED $\to$ ASSIGNED $\to$ STARTED**: Dispatcher assigns driver/vehicle, then starts the trip. There is no CONFIRMED state; driver-app visibility is governed by assignment, not a confirm step.
3. **STARTED $\to$ REACHED_PICKUP**: Manual `ReachPickup`, or automatic on origin-zone entry only when the per-tenant `auto_reach_pickup` flag is on (default OFF).
4. **REACHED_PICKUP $\to$ IN_TRANSIT**: Manual `StartTransit`, or automatic on pickup-exit/drop-entry only when `auto_start_transit` is on (default OFF). No "highway corridor" check exists.
5. **IN_TRANSIT $\to$ DELIVERED**: Manual `Deliver` only — no automatic destination-geofence completion exists.
6. **DELIVERED $\to$ COMPLETED**: `Complete` requires `delivered` and records odometer. Per-stop ePOD enforces OTP+signature, but no 4-digit OTP length check exists and tolls/FASTag are not auto-finalized.

---

## 3. Dynamic Geofence Polygon Engine (`internal/geofence/`)

### Spatial Math & Ray-Casting
- Geofences persist polygons as JSON `[[lat,lng],...]` (`PolygonFromJSON` in `internal/geofence/domain/geofence.go`); no WKT parser exists. Circular buffers use `buffer_metres` (default 20m, `DefaultBufferMetres`).
- The background `DwellWorker` checks incoming GPS telemetry against active geofences using the **Ray-Casting Algorithm**.
- **Debounce Window**: Requires 60s sustained dwell (`DefaultDwellDebounce`) inside the buffer to prevent GPS jitter near boundary edges, with single-miss revert.
- **Automated Actions**:
  - Advances trip status on entry/exit only for ReachPickup/StartTransit and only when the per-tenant auto flags are on (default OFF); no auto Deliver/Complete.
  - Generates detention charge line items past the free wait (default 30min, `DefaultDetentionFree`) at the configured hourly rate (default 0 = never bills).
