-- +goose Up
-- 00153 — DPDP consent ledger (Digital Personal Data Protection Act, 2023).
-- One row per (tenant, user, purpose): grant records notice_version +
-- granted_at at signup; withdrawal stamps withdrawn_at (Login refuses while
-- set); re-grant clears withdrawn_at and bumps granted_at/notice_version.
-- No row (legacy/OAuth/admin-created users) = allowed through, mirroring the
-- tenantActive legacy allowance — only an explicit withdrawal blocks.

CREATE TABLE IF NOT EXISTS user_consents (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL,
    user_id         TEXT NOT NULL,
    purpose         TEXT NOT NULL DEFAULT 'platform_use'
                    CHECK (purpose IN ('platform_use')),
    notice_version  TEXT NOT NULL DEFAULT 'v1',
    granted_at      DATETIME NOT NULL DEFAULT (datetime('now')),
    withdrawn_at    DATETIME,
    created_at      DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at      DATETIME NOT NULL DEFAULT (datetime('now')),
    UNIQUE (tenant_id, user_id, purpose)
);

CREATE INDEX IF NOT EXISTS idx_user_consents_tenant_user
    ON user_consents(tenant_id, user_id);

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_user_consents_tenant_fk_insert
BEFORE INSERT ON user_consents
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for user_consents.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_user_consents_tenant_fk_update
BEFORE UPDATE OF tenant_id ON user_consents
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for user_consents.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS trg_user_consents_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_user_consents_tenant_fk_insert;
DROP INDEX IF EXISTS idx_user_consents_tenant_user;
DROP TABLE IF EXISTS user_consents;
