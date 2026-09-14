-- PG port of 00155_access_reviews.sql | status: PORTABLE | flags: none
-- +goose Up
CREATE TABLE IF NOT EXISTS access_reviews (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id         TEXT NOT NULL,
    role_name       TEXT NOT NULL DEFAULT '',
    period          TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'pending'
                    CHECK (status IN ('pending', 'certified', 'revoked')),
    reviewed_by     TEXT,
    reviewed_at     TIMESTAMPTZ,
    due_at          TIMESTAMPTZ NOT NULL,
    notes           TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_access_reviews_tenant_user_period UNIQUE (tenant_id, user_id, period)
);

CREATE INDEX IF NOT EXISTS idx_access_reviews_tenant_due ON access_reviews(tenant_id, status, due_at);

DROP TRIGGER IF EXISTS trg_access_reviews_tenant_fk_insert ON access_reviews;
CREATE TRIGGER trg_access_reviews_tenant_fk_insert BEFORE INSERT ON access_reviews
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();

DROP TRIGGER IF EXISTS trg_access_reviews_tenant_fk_update ON access_reviews;
CREATE TRIGGER trg_access_reviews_tenant_fk_update BEFORE UPDATE OF tenant_id ON access_reviews
FOR EACH ROW WHEN (NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> '') EXECUTE FUNCTION tenant_fk_guard_fn();

-- +goose Down
DROP TRIGGER IF EXISTS trg_access_reviews_tenant_fk_update ON access_reviews;
DROP TRIGGER IF EXISTS trg_access_reviews_tenant_fk_insert ON access_reviews;
DROP INDEX IF EXISTS idx_access_reviews_tenant_due;
DROP TABLE IF EXISTS access_reviews CASCADE;
