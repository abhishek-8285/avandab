-- PG port of 00165_audit_logs_location.sql | status: PORTABLE | flags: none
-- +goose Up
-- SQL in this section is executed when the migration is applied.

ALTER TABLE audit_logs ADD COLUMN location TEXT;

-- +goose Down
ALTER TABLE audit_logs DROP COLUMN location;
