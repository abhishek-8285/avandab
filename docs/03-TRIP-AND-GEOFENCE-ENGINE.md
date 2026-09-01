# 03. Booking, Trip Execution & Geofence Engine

> **Complete Specification for Dispatch, Trips State Machine, Geofencing & Proof of Delivery**
> Fully autonomous lifecycle from booking intake to geofence detection, detention billing, and ePOD completion.

---

## 1. Booking Lifecycle & Invariants

```text
Draft → Pending → Confirmed → Completed
           ↓
       Cancelled
```

- **Creation**: Requires valid `CustomerID`, `RouteID`, `VehicleType`, `Passengers >= 1`, and `Price >= 0`.
- **Confirmation**: Only `Pending` bookings can be confirmed; moves to `Confirmed`.
- **Immutability**: Once `Completed` or `Cancelled`, no further edits are permitted.
- **Audit**: Every state change is written to the immutable `audit_logs` table.

---

## 2. Trip Execution State Machine

Trips follow a strict, domain-guarded state machine (`internal/trip/domain/aggregate/trip.go`):

```text
DRAFT → SCHEDULED → CONFIRMED → AT_PICKUP → IN_TRANSIT → AT_DELIVERY → COMPLETED
  │          │          │           │            │            │
  └──────────┴──────────┴───────────┴────────────┴────────────┴──► CANCELLED
```

### State Progression Rules:
1. **DRAFT $\to$ SCHEDULED**: Assigning both a Driver and Vehicle advances the trip. The system checks for scheduling time-window conflicts (a driver/vehicle cannot be assigned to overlapping trips).
2. **SCHEDULED $\to$ CONFIRMED**: Dispatcher confirms the trip, making it visible on the driver's mobile app.
3. **CONFIRMED $\to$ AT_PICKUP**: Triggered automatically when vehicle enters the 500m geofence buffer of the origin pickup zone.
4. **AT_PICKUP $\to$ IN_TRANSIT**: Triggered automatically when vehicle departs pickup geofence and enters the highway corridor.
5. **IN_TRANSIT $\to$ AT_DELIVERY**: Triggered automatically upon entering destination customer geofence.
6. **AT_DELIVERY $\to$ COMPLETED**: Delivery completed upon successful electronic consignee signature and 4-digit OTP approval. Tolls, FASTag, and driver kharcha are finalized.

---

## 3. Dynamic Geofence Polygon Engine (`internal/geofence/`)

### Spatial Math & Ray-Casting
- Geofences use **Well-Known Text (WKT)** polygon boundaries with dynamic circular buffers (`buffer_metres`, default 500m).
- The background `DwellWorker` checks incoming GPS telemetry against active geofences using the **Ray-Casting Algorithm**.
- **Debounce Window**: Requires 2 minutes sustained dwell inside the buffer to prevent GPS jitter near boundary edges.
- **Automated Actions**:
  - Automatically advances trip status on entry/exit.
  - Generates detention charge line items if vehicle dwells past free waiting time (e.g. > 2 hours).
