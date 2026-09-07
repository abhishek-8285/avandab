-- PG port of 00079_worker_leases.sql | status: NEEDS-REVIEW | flags: INT-TO-BIGINT | reviewed: YES
-- expires_at holds epoch-MILLIS (>int4max): BIGINT on PG. sqlite INTEGER is
-- 64-bit so the sqlite file stays as-is; same value range both sides.
-- +goose Up
-- Worker leader-election leases so background jobs run on exactly one replica.
-- expires_at is BIGINT epoch-millis for cross-engine portability (SQLite/PG/MySQL).
CREATE TABLE worker_leases (
    name       TEXT PRIMARY KEY,
    holder     TEXT NOT NULL,
    expires_at BIGINT NOT NULL
);
CREATE INDEX idx_worker_leases_expiry ON worker_leases(expires_at);
-- +goose Down
DROP INDEX IF EXISTS idx_worker_leases_expiry;
DROP TABLE IF EXISTS worker_leases;
