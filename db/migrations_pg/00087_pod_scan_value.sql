-- PG port of 00087_pod_scan_value.sql | status: NEEDS-REVIEW | flags: HAS-BACKTICK | reviewed: YES
-- +goose Up
-- 00085: Barcode/QR scan value captured as POD proof (FleetBase Navigator
-- parity: `scan` pod_method). Stored alongside the other e-POD evidence.
ALTER TABLE trips ADD COLUMN pod_scan_value TEXT;
-- +goose Down
ALTER TABLE trips DROP COLUMN pod_scan_value;
