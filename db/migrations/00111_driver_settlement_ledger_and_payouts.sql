-- +goose Up
-- Migration 00111: Driver Settlement Ledger & Payout Orchestration (Phase 8)

CREATE TABLE IF NOT EXISTS driver_ledger_entries (
    id             TEXT PRIMARY KEY,
    tenant_id      TEXT NOT NULL,
    driver_id      TEXT NOT NULL,
    trip_id        TEXT,
    entry_type     TEXT NOT NULL CHECK(entry_type IN ('TRIP_EARNING', 'COMMISSION', 'TOLL_ADJUSTMENT', 'PENALTY', 'PAYOUT', 'PAYOUT_REVERSAL', 'ADVANCE_DEDUCTION', 'BONUS')),
    amount         REAL NOT NULL,
    currency       TEXT NOT NULL DEFAULT 'INR',
    reference_type TEXT NOT NULL,
    reference_id   TEXT NOT NULL,
    balance_after  REAL NOT NULL,
    description    TEXT,
    created_at     DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
    FOREIGN KEY (driver_id) REFERENCES drivers(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_driver_ledger_entries_drv_time
ON driver_ledger_entries(tenant_id, driver_id, created_at);

CREATE TABLE IF NOT EXISTS payout_instructions (
    id                 TEXT PRIMARY KEY,
    tenant_id          TEXT NOT NULL,
    driver_id          TEXT NOT NULL,
    payout_account_id  TEXT,
    amount             REAL NOT NULL,
    currency           TEXT NOT NULL DEFAULT 'INR',
    idempotency_key    TEXT NOT NULL,
    provider_payout_id TEXT,
    status             TEXT NOT NULL DEFAULT 'initiated' CHECK(status IN ('initiated', 'processing', 'paid', 'failed', 'reversed', 'cancelled')),
    failure_reason     TEXT,
    utr                TEXT,
    initiated_at       DATETIME NOT NULL DEFAULT (datetime('now')),
    completed_at       DATETIME,
    created_at         DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at         DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
    FOREIGN KEY (driver_id) REFERENCES drivers(id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_payout_instructions_idemp
ON payout_instructions(tenant_id, idempotency_key);

CREATE INDEX IF NOT EXISTS idx_payout_instructions_driver
ON payout_instructions(tenant_id, driver_id, status);

CREATE TABLE IF NOT EXISTS provider_events (
    id                TEXT PRIMARY KEY,
    tenant_id         TEXT NOT NULL,
    provider          TEXT NOT NULL,
    provider_event_id TEXT NOT NULL,
    event_type        TEXT NOT NULL,
    payload           TEXT NOT NULL,
    processed_at      DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_provider_events_provider_id
ON provider_events(provider, provider_event_id);

-- +goose Down
DROP INDEX IF EXISTS idx_provider_events_provider_id;
DROP TABLE IF EXISTS provider_events;
DROP INDEX IF EXISTS idx_payout_instructions_driver;
DROP INDEX IF EXISTS idx_payout_instructions_idemp;
DROP TABLE IF EXISTS payout_instructions;
DROP INDEX IF EXISTS idx_driver_ledger_entries_drv_time;
DROP TABLE IF EXISTS driver_ledger_entries;
