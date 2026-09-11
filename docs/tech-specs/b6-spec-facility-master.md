# Spec: Facility Master & ZFID Sync (B6)

- **Migration Slot:** `00145` (Allocated in ownership index)
- **Owner:** Domain: `internal/facility` | Handlers: `internal/handlers/facilities.go` | Route: `/api/v1/facilities`
- **Status:** Approved Spec (Roadmap B6)
- **Reference:** SAP TMS SOP pp.1-2 (`ZFID` Facility Lookup & Facility Master Data)

## 1. Operational Rationale & Acceptance Criteria
In standard logistics and fleet operations (TMS SOP pp.1-2), every physical operating depot, hub, workshop, fuel station, or administrative division is designated as a **Facility** (`facility_id`, e.g. `MM21000000757`).
- Vehicles link to an owning/operating facility (`vehicles.facility_id`).
- Trips depart and arrive at designated facility gates (`trips.gate_facility_id`).
- Fuel dispensers and measuring points operate within facility boundaries.

Acceptance criteria:
1. Canonical `facilities` table partitioned by `tenant_id` with unique `(tenant_id, facility_code)`.
2. Master attributes matching SAP p.2 screenshot:
   - Facility ID / Code (`facility_code`, e.g., `MM21000000757`)
   - Description / Name (`name`, e.g., `MMS Office - Bangalore`, `Pune North Depot`)
   - Type (`facility_type`: `depot`, `hub`, `branch`, `workshop`, `office`, `fuel_station`)
   - Plant / Maint Plant (`plant`)
   - Circle / Business Area (`circle`)
   - Profit Center (`profit_center`)
   - Cost Center (`cost_center`)
   - Address / Geolocation (`address`, `city`, `state`, `pincode`, `latitude`, `longitude`)
   - Validity Range (`valid_from`, `valid_to`, `is_active`)
3. Tenant trigger enforcement (00103/00104 conventions).
4. REST API mounted at `/api/v1/facilities` with RBAC permission `facilities:read` and `facilities:write`.
5. Full dual-engine migration support (SQLite + PostgreSQL).

## 2. Schema Specification (`00145`)

```sql
CREATE TABLE IF NOT EXISTS facilities (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL,
    facility_code   TEXT NOT NULL,
    name            TEXT NOT NULL,
    facility_type   TEXT NOT NULL DEFAULT 'depot'
                    CHECK (facility_type IN ('depot', 'hub', 'branch', 'workshop', 'office', 'fuel_station')),
    plant           TEXT NOT NULL DEFAULT '',
    circle          TEXT NOT NULL DEFAULT '',
    profit_center   TEXT NOT NULL DEFAULT '',
    cost_center     TEXT NOT NULL DEFAULT '',
    address         TEXT NOT NULL DEFAULT '',
    city            TEXT NOT NULL DEFAULT '',
    state           TEXT NOT NULL DEFAULT '',
    pincode         TEXT NOT NULL DEFAULT '',
    latitude        REAL,
    longitude       REAL,
    valid_from      DATE,
    valid_to        DATE,
    is_active       INTEGER NOT NULL DEFAULT 1,
    created_at      DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at      DATETIME NOT NULL DEFAULT (datetime('now')),
    UNIQUE (tenant_id, facility_code)
);
```

## 3. REST API Specification
- `GET /api/v1/facilities`: List facilities with search query (`?q=`), type filter (`?type=`), and pagination (`?limit=50&offset=0`).
- `POST /api/v1/facilities`: Create facility.
- `GET /api/v1/facilities/{id}`: Get single facility by ID or facility_code.
- `PUT /api/v1/facilities/{id}`: Update facility metadata.
