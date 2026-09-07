-- PG port of 00067_eta_history.sql | status: PORTABLE | flags: none
-- +goose Up
-- 00067: ETA history + monthly aggregation (Spec 18 Wave A)
-- 90-day raw retention, then monthly rollup (see internal/eta/cleanup.go).
-- Segment key is TEXT (route_id or geohash) — provider-agnostic.

CREATE TABLE IF NOT EXISTS eta_history (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL DEFAULT '1',
    trip_id TEXT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    segment_start TEXT NOT NULL,
    segment_end TEXT NOT NULL,
    actual_minutes INTEGER NOT NULL CHECK (actual_minutes > 0),
    traffic_tag TEXT CHECK (traffic_tag IN ('low','medium','high','monsoon')),
    day_of_week INTEGER CHECK (day_of_week BETWEEN 0 AND 6),
    hour_of_day INTEGER CHECK (hour_of_day BETWEEN 0 AND 23),
    created_at TIMESTAMPTZ NOT NULL DEFAULT (CURRENT_TIMESTAMP)
);
CREATE TABLE IF NOT EXISTS eta_history_monthly (
    tenant_id TEXT NOT NULL DEFAULT '1',
    segment_start TEXT NOT NULL,
    segment_end TEXT NOT NULL,
    month TEXT NOT NULL, -- YYYY-MM-01
    avg_minutes REAL NOT NULL,
    sample_count INTEGER NOT NULL,
    PRIMARY KEY (tenant_id, segment_start, segment_end, month)
);
CREATE INDEX IF NOT EXISTS idx_eta_history_segment ON eta_history(tenant_id, segment_start, segment_end, created_at);
CREATE INDEX IF NOT EXISTS idx_eta_history_trip ON eta_history(trip_id);
CREATE INDEX IF NOT EXISTS idx_eta_history_created ON eta_history(created_at);
-- +goose Down
DROP TABLE IF EXISTS eta_history_monthly;
DROP TABLE IF EXISTS eta_history;
