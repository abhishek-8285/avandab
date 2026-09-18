-- +goose Up
-- 00164 — Subscriber idempotency backstop (outbox at-least-once retry safety).
-- Duplicate invoice deliveries must converge instead of minting duplicate
-- rows. The application pre-check (GetInvoiceByTripID) handles the sequential
-- case; this partial unique index closes the concurrent double-delivery
-- window at the database level. NULL/'' means "no link" and is never
-- deduplicated. Local audit at authoring time found zero duplicate trip
-- invoices; if this migration fails on a database, that database already
-- holds duplicates needing manual cleanup — do not weaken the constraint.
--
-- Deliberately scoped to invoices only: trips are 1:N per booking in this
-- codebase (e.g. test/settlement_engine_test.go seeds trip-stl-1 and
-- trip-stl-2 under one booking), so a UNIQUE(tenant, booking) constraint
-- would forbid legitimate data. Trip duplicates stay guarded by the
-- application pre-check (GetTripByBookingID) plus single-threaded relay
-- dispatch.

CREATE UNIQUE INDEX IF NOT EXISTS idx_invoices_tenant_trip_unique
    ON invoices(tenant_id, trip_id)
    WHERE trip_id IS NOT NULL AND trip_id != '';

-- +goose Down
DROP INDEX IF EXISTS idx_invoices_tenant_trip_unique;
