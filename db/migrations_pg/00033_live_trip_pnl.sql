-- PG port of 00033_live_trip_pnl.sql | status: PORTABLE | flags: none
-- +goose Up
ALTER TABLE trips ADD COLUMN estimated_margin REAL NOT NULL DEFAULT 0;
ALTER TABLE trips ADD COLUMN fuel_consumed_liters REAL NOT NULL DEFAULT 0;
ALTER TABLE trips ADD COLUMN toll_costs REAL NOT NULL DEFAULT 0;
ALTER TABLE trips ADD COLUMN last_pnl_update TIMESTAMPTZ;
CREATE TABLE IF NOT EXISTS fuel_prices (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL DEFAULT '1',
    state TEXT NOT NULL,
    city TEXT,
    diesel_price REAL NOT NULL DEFAULT 0,
    petrol_price REAL NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_fuel_prices_lookup
    ON fuel_prices (tenant_id, state, city, updated_at DESC);
-- +goose Down
DROP INDEX IF EXISTS idx_fuel_prices_lookup;
DROP TABLE IF EXISTS fuel_prices;
-- SQLite cannot drop columns on older supported versions; retained columns are harmless on rollback.
