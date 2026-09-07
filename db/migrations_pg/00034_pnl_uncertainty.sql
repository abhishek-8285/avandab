-- PG port of 00034_pnl_uncertainty.sql | status: PORTABLE | flags: none
-- +goose Up
ALTER TABLE trips ADD COLUMN fuel_cost_low REAL NOT NULL DEFAULT 0;
ALTER TABLE trips ADD COLUMN fuel_cost_high REAL NOT NULL DEFAULT 0;
ALTER TABLE trips ADD COLUMN margin_low REAL NOT NULL DEFAULT 0;
ALTER TABLE trips ADD COLUMN margin_high REAL NOT NULL DEFAULT 0;
ALTER TABLE trips ADD COLUMN pnl_confidence TEXT NOT NULL DEFAULT 'unavailable';
ALTER TABLE trips ADD COLUMN fuel_cost_status TEXT NOT NULL DEFAULT 'pending_verification';
-- +goose Down
-- SQLite column removal is intentionally omitted for compatibility.
