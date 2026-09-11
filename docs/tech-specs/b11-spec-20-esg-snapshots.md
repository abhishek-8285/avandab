# Spec 20 §3: ESG Emission Snapshots & Scope 3 Carbon Accounting (B11)

- **Migration Slot:** `00148` (Re-slotted from `00072` reserved slot per sequential migration rule)
- **Owner:** Domain: `internal/sustainability/application` & `internal/handlers/esg.go` | Route: `/api/v1/esg/*`
- **Status:** Approved Spec (Roadmap B11)

## 1. Problem Statement & Regulatory Mandate
Enterprise logistics contracts in India increasingly require BRSR (Business Responsibility and Sustainability Reporting) Core compliance and GHG Protocol Corporate Value Chain (Scope 3) Category 4 emissions reporting. Transporters without automated carbon accounting face commercial disqualification in Fortune 500 / NSE top-1000 supply chain RFPs.

## 2. Emission Accounting Methodology
1. **Primary Calculation (Fuel-Based Method - High Accuracy):**
   $$\text{CO}_2\text{e (kg)} = \text{Total Fuel Consumed (Litres)} \times \text{Emission Factor}$$
   - High-Speed Diesel (HSD): $2.68\text{ kg CO}_2\text{e / Litre}$
   - Compressed Natural Gas (CNG): $2.75\text{ kg CO}_2\text{e / kg}$
2. **Secondary Calculation (Distance-Tonne-Km Activity Method - Fallback):**
   $$\text{Cargo Tonne-Km (tkm)} = \text{Distance Covered (km)} \times \text{Consignment Net Payload (Tonnes)}$$
   - BS-IV Heavy Commercial Vehicles: $115\text{ g CO}_2\text{e / tkm}$
   - BS-VI Heavy Commercial Vehicles: $92\text{ g CO}_2\text{e / tkm}$
   - Electric Vehicles (EV): $0\text{ g tailpipe}$; upstream grid emission factored at regional grid carbon intensity ($0.82\text{ kg CO}_2\text{e / kWh}$).

## 3. Schema Architecture (`00072`)
```sql
CREATE TABLE trip_esg_metrics (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    trip_id TEXT NOT NULL REFERENCES trips(id),
    distance_km REAL NOT NULL CHECK (distance_km >= 0),
    payload_tonnes REAL NOT NULL CHECK (payload_tonnes >= 0),
    fuel_consumed_litres REAL NOT NULL DEFAULT 0,
    co2e_kg REAL NOT NULL CHECK (co2e_kg >= 0),
    co2e_per_tkm REAL NOT NULL DEFAULT 0,
    emission_norm TEXT NOT NULL CHECK (emission_norm IN ('BS3', 'BS4', 'BS6', 'EV', 'CNG', 'OTHER')),
    methodology TEXT NOT NULL DEFAULT 'FUEL_PRIMARY' CHECK (methodology IN ('FUEL_PRIMARY', 'DISTANCE_ACTIVITY', 'DEFAULT_FACTOR')),
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(tenant_id, trip_id)
);

CREATE TABLE esg_emission_snapshots (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    period_start TEXT NOT NULL,
    period_end TEXT NOT NULL,
    total_trips INTEGER NOT NULL CHECK (total_trips >= 0),
    total_distance_km REAL NOT NULL CHECK (total_distance_km >= 0),
    total_cargo_tkm REAL NOT NULL CHECK (total_cargo_tkm >= 0),
    total_fuel_litres REAL NOT NULL CHECK (total_fuel_litres >= 0),
    total_co2e_kg REAL NOT NULL CHECK (total_co2e_kg >= 0),
    avg_co2e_per_tkm REAL NOT NULL CHECK (avg_co2e_per_tkm >= 0),
    ev_distance_km REAL NOT NULL DEFAULT 0,
    bs6_distance_km REAL NOT NULL DEFAULT 0,
    bs4_distance_km REAL NOT NULL DEFAULT 0,
    created_by TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(tenant_id, period_start, period_end)
);

CREATE INDEX idx_trip_esg_tenant ON trip_esg_metrics(tenant_id, created_at);
CREATE INDEX idx_esg_snapshots_period ON esg_emission_snapshots(tenant_id, period_start, period_end);
```

## 4. API Endpoints
- `GET /api/v1/esg/trips/{trip_id}` — Trip-level carbon audit certificate.
- `GET /api/v1/esg/snapshots` — List monthly/quarterly aggregated emission snapshots.
- `POST /api/v1/esg/snapshots/generate` — Trigger batch aggregation for a specified date range.
- `GET /api/v1/esg/reports/brsr` — Export BRSR Section C Principle 6 ESG summary (JSON / CSV).

## 5. Security & Verification
- All queries strictly enforce `tenant_id` isolation from context.
- Historical snapshots are immutable; any recalculation archives the prior record and generates a versioned superseding snapshot.

## 6. Acceptance Criteria
1. DDL applies cleanly with forward and backward migrations in SQLite and PostgreSQL.
2. Tenant FK trigger guards ensure zero cross-tenant contamination.
3. Zero-division guard: Trips with 0 km or 0 tonnes calculate without NaN/Inf errors.
