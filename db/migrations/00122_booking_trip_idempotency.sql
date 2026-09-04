-- +goose Up
-- 00122 — Idempotency keys for booking/trip creation (P6 retry safety).
-- Retried POSTs (timeouts, LB replays, double clicks) must not mint duplicate
-- BK/TR rows. Keys are client-supplied, unique per tenant; NULL/'' means
-- "no key" and is never deduplicated. Mirrors 00076 expense precedent but
-- tenant-scoped (two orgs may legitimately send the same key).

ALTER TABLE bookings ADD COLUMN idempotency_key TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS idx_bookings_idempotency
    ON bookings(tenant_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL AND idempotency_key != '';

ALTER TABLE trips ADD COLUMN idempotency_key TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS idx_trips_idempotency
    ON trips(tenant_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL AND idempotency_key != '';

-- +goose Down
DROP INDEX IF EXISTS idx_trips_idempotency;
ALTER TABLE trips DROP COLUMN idempotency_key;
DROP INDEX IF EXISTS idx_bookings_idempotency;
ALTER TABLE bookings DROP COLUMN idempotency_key;
