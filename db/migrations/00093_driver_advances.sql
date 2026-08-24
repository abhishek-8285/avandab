-- +goose Up
-- Spec 22 S7 — driver advance requests (Paisa tab).
-- Deviation from spec §3 SQL (documented): extra nullable settlement_id
-- column implements §5.5 linkage — an approved advance attaches to the
-- next settlement that includes it and flips to 'paid' when that
-- settlement is marked paid (edge case 8: pending advances never deducted).
CREATE TABLE driver_advance_requests (
  id            TEXT PRIMARY KEY,
  tenant_id     TEXT NOT NULL,
  driver_id     TEXT NOT NULL,
  trip_id       TEXT,
  amount        REAL NOT NULL CHECK (amount > 0),
  reason        TEXT NOT NULL DEFAULT '',
  status        TEXT NOT NULL DEFAULT 'pending'
                CHECK (status IN ('pending','approved','rejected','paid')),
  requested_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  decided_by    TEXT,
  decided_at    TIMESTAMP,
  settlement_id TEXT
);
CREATE INDEX idx_advances_tenant_status
  ON driver_advance_requests(tenant_id, status, requested_at DESC);
CREATE INDEX idx_advances_driver_status
  ON driver_advance_requests(driver_id, status);
CREATE INDEX idx_advances_settlement
  ON driver_advance_requests(settlement_id);

-- +goose Down
DROP INDEX IF EXISTS idx_advances_settlement;
DROP INDEX IF EXISTS idx_advances_driver_status;
DROP INDEX IF EXISTS idx_advances_tenant_status;
DROP TABLE IF EXISTS driver_advance_requests;
