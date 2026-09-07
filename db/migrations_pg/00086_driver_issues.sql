-- PG port of 00086_driver_issues.sql | status: NEEDS-REVIEW | flags: HAS-DQUOTE | reviewed: YES
-- +goose Up
-- 00084: Driver-reported issues (FleetBase Navigator parity: "Report Issues"
-- screen). Drivers flag vehicle/road/cargo problems from the mobile app with
-- severity, category, optional trip reference and optional photo.
CREATE TABLE IF NOT EXISTS driver_issues (
    id         TEXT PRIMARY KEY,
    tenant_id  TEXT NOT NULL DEFAULT '1',
    driver_id  TEXT NOT NULL,
    trip_id    TEXT,
    category   TEXT NOT NULL DEFAULT 'other'
               CHECK (category IN ('vehicle', 'road', 'cargo', 'customer', 'accident', 'other')),
    severity   TEXT NOT NULL DEFAULT 'low'
               CHECK (severity IN ('low', 'medium', 'high', 'critical')),
    message    TEXT NOT NULL,
    photo_url  TEXT,
    status     TEXT NOT NULL DEFAULT 'open'
               CHECK (status IN ('open', 'acknowledged', 'resolved')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT (CURRENT_TIMESTAMP)
);
CREATE INDEX IF NOT EXISTS idx_driver_issues_driver ON driver_issues(driver_id, created_at);
-- +goose Down
DROP TABLE IF EXISTS driver_issues;
