-- +goose Up
-- 00155 — periodic access re-certification ledger (UN-style 6/12-month
-- access reviews). One row per (tenant, user, period): open a pending row
-- for the review period (e.g. '2026-H2'), then certify (access still needed)
-- or revoke (access withdrawn + note). due_at is reviewer-chosen per row;
-- the due watchlist (status pending AND due_at <= now) derives from it in
-- queries. Routes reuse privacy:manage (00154 backfill, roles 1 + 6), so no
-- new permission rows ship here.

CREATE TABLE IF NOT EXISTS access_reviews (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL,
    user_id         TEXT NOT NULL,
    role_name       TEXT NOT NULL DEFAULT '',
    period          TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'pending'
                    CHECK (status IN ('pending', 'certified', 'revoked')),
    reviewed_by     TEXT,
    reviewed_at     DATETIME,
    due_at          DATETIME NOT NULL,
    notes           TEXT NOT NULL DEFAULT '',
    created_at      DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at      DATETIME NOT NULL DEFAULT (datetime('now')),
    UNIQUE (tenant_id, user_id, period)
);

CREATE INDEX IF NOT EXISTS idx_access_reviews_tenant_due
    ON access_reviews(tenant_id, status, due_at);

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_access_reviews_tenant_fk_insert
BEFORE INSERT ON access_reviews
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for access_reviews.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_access_reviews_tenant_fk_update
BEFORE UPDATE OF tenant_id ON access_reviews
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for access_reviews.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS trg_access_reviews_tenant_fk_update;
DROP TRIGGER IF EXISTS trg_access_reviews_tenant_fk_insert;
DROP INDEX IF EXISTS idx_access_reviews_tenant_due;
DROP TABLE IF EXISTS access_reviews;
