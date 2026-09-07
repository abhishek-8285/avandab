-- PG port of 00074_churn_driver_offline.sql | status: PORTABLE | flags: none
-- +goose Up
-- 00074_churn_driver_offline.sql
-- offline: no DB change (mobile SQLite). Server side: expense table already exists via kharcha.
-- Add expense type check widening + offline sync log
CREATE TABLE IF NOT EXISTS offline_sync_log (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL DEFAULT '1',
    user_id TEXT NOT NULL,
    kind TEXT NOT NULL, -- pod | expense | gps
    payload TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT,
    created_at TEXT NOT NULL DEFAULT (CURRENT_TIMESTAMP)
);
-- +goose Down
DROP TABLE IF EXISTS offline_sync_log;
