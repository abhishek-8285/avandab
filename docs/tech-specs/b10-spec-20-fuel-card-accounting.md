# Spec 20 §2: Commercial Fuel Cards & Accounting Integration (B10)

- **Migration Slot:** `00071` (Allocated in ownership index)
- **Owner:** Domain: `internal/fuel/application` & `internal/handlers/fuel_cards.go` | Route: `/api/v1/fuel-cards/*`
- **Status:** Approved Spec (Roadmap B10)

## 1. Problem Statement & Financial Rationale
Fuel expenses comprise 45–55% of operational operating costs for Indian commercial fleets. Fleets rely on fleet fuel cards (IOCL XTRAPOWER, BPCL SmartFleet, HPCL DriveTrack Plus) to eliminate cash leakage. Manual reconciliation between oil marketing company (OMC) statement feeds, driver expense claims (`driver_expenses`), and general ledger accounts currently causes delayed accounting closes and undiscovered fuel pilferage.

## 2. Schema Architecture (`00071`)
```sql
CREATE TABLE fuel_cards (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    card_number_masked TEXT NOT NULL,
    card_token_hash TEXT NOT NULL,
    provider TEXT NOT NULL CHECK (provider IN ('IOCL', 'BPCL', 'HPCL', 'SHELL', 'FLEET_BANK')),
    assigned_vehicle_id TEXT REFERENCES vehicles(id),
    assigned_driver_id TEXT REFERENCES drivers(id),
    daily_spend_limit REAL NOT NULL DEFAULT 50000 CHECK (daily_spend_limit >= 0),
    status TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'SUSPENDED', 'CANCELLED')),
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(tenant_id, card_token_hash)
);

CREATE TABLE fuel_card_transactions (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    fuel_card_id TEXT NOT NULL REFERENCES fuel_cards(id),
    external_txn_id TEXT NOT NULL,
    txn_time TEXT NOT NULL,
    fuel_station_name TEXT NOT NULL,
    fuel_station_city TEXT,
    fuel_type TEXT NOT NULL DEFAULT 'DIESEL' CHECK (fuel_type IN ('DIESEL', 'PETROL', 'CNG', 'DEF', 'LUBRICANT')),
    volume_litres REAL NOT NULL CHECK (volume_litres > 0),
    rate_per_litre REAL NOT NULL CHECK (rate_per_litre > 0),
    total_amount REAL NOT NULL CHECK (total_amount > 0),
    odometer_reported REAL,
    reconciliation_status TEXT NOT NULL DEFAULT 'UNRECONCILED' 
        CHECK (reconciliation_status IN ('UNRECONCILED', 'MATCHED_EXPENSE', 'SYSTEM_GENERATED', 'FLAGGED_ANOMALY')),
    matched_expense_id TEXT REFERENCES driver_expenses(id),
    sync_log_id TEXT REFERENCES accounting_sync_logs(id),
    notes TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(tenant_id, external_txn_id)
);

CREATE INDEX idx_fuel_cards_tenant ON fuel_cards(tenant_id, status);
CREATE INDEX idx_fuel_txns_tenant_time ON fuel_card_transactions(tenant_id, txn_time);
CREATE INDEX idx_fuel_txns_recon ON fuel_card_transactions(tenant_id, reconciliation_status);
```

## 3. Automated Reconciliation & Audit Loop
1. **OMC Feed / Statement Ingestion:** Transactions are posted via API or monthly CSV upload.
2. **Kharcha Cross-Check (`00094`):**
   - System searches `driver_expenses` for matching date ($\pm 24\text{ hours}$), vehicle, and amount ($\pm ₹10$).
   - If matched: Links `matched_expense_id`, updates expense status to `VERIFIED_FUEL_CARD`, and updates transaction to `MATCHED_EXPENSE`.
   - If driver never filed an expense: Auto-creates a verified `driver_expenses` entry credited to fuel card clearing.
3. **Tank Capacity & Pilferage Guard:**
   - If `volume_litres > vehicles.fuel_tank_capacity * 1.05`: Immediately flags `FLAGGED_ANOMALY` and creates an alert in `ops_alerts` (`00045`).
4. **General Ledger Sync:**
   - Pushes double-entry transaction to `money_ledger` (`00097`):
     - `DEBIT`: Fuel Expense Account (Cost Center: Vehicle Registration).
     - `CREDIT`: Fuel Card Clearing / OMC Vendor Account.

## 4. API Surface
- `POST /api/v1/fuel-cards` — Register fleet card and bind to vehicle/driver.
- `GET /api/v1/fuel-cards` — List active cards with spend metrics.
- `POST /api/v1/fuel-cards/transactions/sync` — Bulk ingest OMC statement transactions.
- `POST /api/v1/fuel-cards/transactions/{id}/reconcile` — Manual link/override against expense claim.

## 5. Acceptance Criteria
1. DDL applies cleanly with forward and rollback migrations in both SQLite and PostgreSQL engines.
2. Trigger-based tenant FK enforcement prevents linking cross-tenant cards or expenses.
3. Reconciled fuel transactions balance to zero variance against accounting sync log entries.
