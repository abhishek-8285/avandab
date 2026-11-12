-- +goose Up
-- 00179 — DPDP Data Subject Access Request (DSAR) and erasure ledger
-- (Digital Personal Data Protection Act, 2023 §11–15: Right to access,
-- correction, and erasure). Records export requests, erasure events,
-- and compliance audits per tenant and principal.

CREATE TABLE IF NOT EXISTS privacy_dsar_requests (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL,
    user_id         TEXT NOT NULL,
    request_type    TEXT NOT NULL CHECK (request_type IN ('export', 'erasure', 'rectification')),
    status          TEXT NOT NULL DEFAULT 'completed' CHECK (status IN ('pending', 'processing', 'completed', 'rejected')),
    requested_by    TEXT NOT NULL,
    details         TEXT NOT NULL DEFAULT '',
    created_at      DATETIME NOT NULL DEFAULT (datetime('now')),
    completed_at    DATETIME
);

CREATE INDEX IF NOT EXISTS idx_privacy_dsar_requests_tenant_user
    ON privacy_dsar_requests(tenant_id, user_id, request_type);

CREATE INDEX IF NOT EXISTS idx_privacy_dsar_requests_tenant_created
    ON privacy_dsar_requests(tenant_id, created_at DESC);

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_privacy_dsar_requests_tenant_fk_insert
BEFORE INSERT ON privacy_dsar_requests
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for privacy_dsar_requests.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_privacy_dsar_requests_tenant_fk_update
BEFORE UPDATE OF tenant_id ON privacy_dsar_requests
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for privacy_dsar_requests.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS trg_privacy_dsar_requests_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_privacy_dsar_requests_tenant_fk_insert;
DROP INDEX IF EXISTS idx_privacy_dsar_requests_tenant_created;
DROP INDEX IF EXISTS idx_privacy_dsar_requests_tenant_user;
DROP TABLE IF EXISTS privacy_dsar_requests;
