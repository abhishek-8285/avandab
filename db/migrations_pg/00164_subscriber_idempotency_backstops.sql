-- +goose Up
-- 00164: subscriber idempotency backstop — PG port of 00164_subscriber_idempotency_backstops.sql
-- Partial unique index closes the concurrent double-invoice window.
-- NULL/'' means "no link" and is never deduplicated. Scoped to invoices
-- only: trips are 1:N per booking, so no trips constraint.

CREATE UNIQUE INDEX IF NOT EXISTS idx_invoices_tenant_trip_unique
    ON invoices(tenant_id, trip_id)
    WHERE trip_id IS NOT NULL AND trip_id <> '';

-- +goose Down
DROP INDEX IF EXISTS idx_invoices_tenant_trip_unique;
