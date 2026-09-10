# Spec 19 §2: Stock Transfer Order (STO) Portal & Load Board Listings (B8)

- **Migration Slot:** `00069` (Allocated in ownership index)
- **Owner:** Domain: `internal/sto/application` & `internal/handlers/sto.go` | Route: `/api/v1/sto/*`, `/api/v1/loadboard/*`
- **Status:** Approved Spec (Roadmap B8)

## 1. Problem Statement & Business Objective
Enterprises with distributed manufacturing plants and Regional Distribution Centers (RDCs) execute internal Stock Transfer Orders (STOs) under GST rules without invoicing a commercial sale (using delivery challans and E-Way Bills). When private fleet capacity is insufficient, unassigned STOs must be syndicated to an internal or federated load board where approved external carriers can submit bids and accept assignments.

## 2. Data Architecture & Schema (`00069`)
```sql
-- stock_transfer_orders
CREATE TABLE stock_transfer_orders (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    sto_number TEXT NOT NULL,
    origin_facility_id TEXT NOT NULL,
    destination_facility_id TEXT NOT NULL,
    material_code TEXT NOT NULL,
    material_description TEXT NOT NULL,
    quantity REAL NOT NULL CHECK (quantity > 0),
    uom TEXT NOT NULL,
    required_delivery_date TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'DRAFT' CHECK (status IN ('DRAFT', 'RELEASED', 'POSTED', 'ASSIGNED', 'IN_TRANSIT', 'RECEIVED', 'CANCELLED')),
    notes TEXT,
    created_by TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(tenant_id, sto_number)
);

-- load_board_listings
CREATE TABLE load_board_listings (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    sto_id TEXT REFERENCES stock_transfer_orders(id),
    booking_id TEXT REFERENCES bookings(id),
    origin_city TEXT NOT NULL,
    destination_city TEXT NOT NULL,
    vehicle_type_required TEXT NOT NULL,
    target_rate REAL NOT NULL CHECK (target_rate >= 0),
    max_rate REAL NOT NULL CHECK (max_rate >= target_rate),
    visibility TEXT NOT NULL DEFAULT 'PRIVATE' CHECK (visibility IN ('PRIVATE', 'FEDERATED', 'PUBLIC')),
    status TEXT NOT NULL DEFAULT 'OPEN' CHECK (status IN ('OPEN', 'BIDDING', 'AWARDED', 'EXPIRED', 'CANCELLED')),
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    CHECK (sto_id IS NOT NULL OR booking_id IS NOT NULL)
);

CREATE INDEX idx_sto_tenant_status ON stock_transfer_orders(tenant_id, status, required_delivery_date);
CREATE INDEX idx_loadboard_tenant_status ON load_board_listings(tenant_id, status, expires_at);
```
*(Includes standard 00103 tenant trigger checks on INSERT/UPDATE).*

## 3. Lifecycle & State Transitions
```
   [ Create STO (DRAFT) ]
              │
              ▼
    [ Release STO (RELEASED) ]
              │
      ┌───────┴───────────────────────┐
      ▼                               ▼
[ Assign Internal Fleet ]    [ Post to Load Board (POSTED) ]
      │                               │
      │                               ▼
      │                     [ Carrier Bidding (OPEN) ]
      │                               │
      │                               ▼
      │                     [ Award Bid (AWARDED) ]
      │                               │
      └───────┬───────────────────────┘
              ▼
   [ Dispatched / In Transit ]
              │
              ▼
   [ Delivered / Received at Destination ]
              │
              ▼
     [ Closed / Reconciled ]
```

## 4. API Endpoints
- `POST /api/v1/sto` — Create STO draft.
- `POST /api/v1/sto/{id}/release` — Release for dispatch or load board syndication.
- `POST /api/v1/sto/{id}/post-to-loadboard` — Publish listing to load board.
- `GET /api/v1/loadboard/listings` — List open loads (tenant-scoped; federated if authorized).
- `POST /api/v1/loadboard/listings/{id}/bids` — Submit carrier quote/bid.
- `POST /api/v1/loadboard/listings/{id}/award` — Award bid to winning transporter; spawns booking & trip.

## 5. Security, Tax & Compliance
- **Delivery Challan:** Under Rule 55 of CGST Rules, STOs generate delivery challans with `E-Way Bill` integration (`00047/00127`).
- **Multi-Tenant Isolation:** Private listings are never exposed across tenant boundaries; federated listings expose only anonymized corridor/capacity details until carrier award.

## 6. Acceptance Criteria
1. DDL applies cleanly with forward and backward migrations in SQLite and PostgreSQL.
2. Tenant FK trigger guards prevent cross-tenant referencing between `load_board_listings` and `stock_transfer_orders`.
3. Bidding automatically locks once an award transaction executes; concurrent award attempts fail-closed.
