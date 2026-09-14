-- PG port of 00153_user_consents.sql | status: PORTABLE | flags: none
-- +goose Up
CREATE TABLE IF NOT EXISTS user_consents (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id         TEXT NOT NULL,
    purpose         TEXT NOT NULL DEFAULT 'platform_use'
                    CHECK (purpose IN ('platform_use')),
    notice_version  TEXT NOT NULL DEFAULT 'v1',
    granted_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    withdrawn_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_user_consents_tenant_user_purpose UNIQUE (tenant_id, user_id, purpose)
);

CREATE INDEX IF NOT EXISTS idx_user_consents_tenant_user ON user_consents(tenant_id, user_id);

DROP TRIGGER IF EXISTS trg_user_consents_tenant_fk_insert ON user_consents;
CREATE TRIGGER trg_user_consents_tenant_fk_insert BEFORE INSERT ON user_consents
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();

DROP TRIGGER IF EXISTS trg_user_consents_tenant_fk_update ON user_consents;
CREATE TRIGGER trg_user_consents_tenant_fk_update BEFORE UPDATE OF tenant_id ON user_consents
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();

-- +goose Down
DROP TRIGGER IF EXISTS trg_user_consents_tenant_fk_update ON user_consents;
DROP TRIGGER IF EXISTS trg_user_consents_tenant_fk_insert ON user_consents;
DROP INDEX IF EXISTS idx_user_consents_tenant_user;
DROP TABLE IF EXISTS user_consents CASCADE;
