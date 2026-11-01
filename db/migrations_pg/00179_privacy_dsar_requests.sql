-- +goose Up
-- PG port of 00179_privacy_dsar_requests.sql
-- DPDP Data Subject Access Request (DSAR) and erasure ledger
-- (Digital Personal Data Protection Act, 2023 §11–15: Right to access,
-- correction, and erasure).

CREATE TABLE IF NOT EXISTS privacy_dsar_requests (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id         TEXT NOT NULL,
    request_type    TEXT NOT NULL CHECK (request_type IN ('export', 'erasure', 'rectification')),
    status          TEXT NOT NULL DEFAULT 'completed' CHECK (status IN ('pending', 'processing', 'completed', 'rejected')),
    requested_by    TEXT NOT NULL,
    details         TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at    TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_privacy_dsar_requests_tenant_user
    ON privacy_dsar_requests(tenant_id, user_id, request_type);

CREATE INDEX IF NOT EXISTS idx_privacy_dsar_requests_tenant_created
    ON privacy_dsar_requests(tenant_id, created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_privacy_dsar_requests_tenant_created;
DROP INDEX IF EXISTS idx_privacy_dsar_requests_tenant_user;
DROP TABLE IF EXISTS privacy_dsar_requests;
