-- PG port of 00084_error_reports_request_meta.sql | status: PORTABLE | flags: none
-- +goose Up
-- Spec 16 §5.5 carry-over #2: persist correlation id + client capture payload
-- on error_reports so support can trace a user-reported ref to its log entry.
-- request_id = X-Request-ID of the failing request (or client-supplied ref);
-- metadata   = JSON blob (breadcrumbs, viewport, source) from client reports.

ALTER TABLE error_reports ADD COLUMN request_id TEXT NOT NULL DEFAULT '';
ALTER TABLE error_reports ADD COLUMN metadata TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_error_reports_request ON error_reports (request_id);
-- +goose Down
DROP INDEX IF EXISTS idx_error_reports_request;
ALTER TABLE error_reports DROP COLUMN metadata;
ALTER TABLE error_reports DROP COLUMN request_id;
