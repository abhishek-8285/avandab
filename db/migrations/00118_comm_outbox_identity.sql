-- +goose Up
-- 00118 — Comm outbox + external identity (Spec 06 auth / zero-cost comms plan).
--
-- users gains external identity columns: auth_provider tracks how the account
-- authenticates ('password' | 'google'), google_sub stores the Google OIDC
-- subject (unique when present), phone_verified_at stamps Firebase phone
-- verification (Phase 4, driver app).
ALTER TABLE users ADD COLUMN auth_provider TEXT NOT NULL DEFAULT 'password';
ALTER TABLE users ADD COLUMN google_sub TEXT;
ALTER TABLE users ADD COLUMN phone_verified_at DATETIME;

CREATE UNIQUE INDEX IF NOT EXISTS idx_users_google_sub
    ON users(google_sub) WHERE google_sub IS NOT NULL;

-- comm_outbox is the single durable outbound queue for email and WhatsApp.
-- Producers insert in the SAME transaction as the business write (true
-- outbox — no lost sends); the worker polls (status, next_attempt_at),
-- applies exponential backoff via attempts, and dead-letters after max
-- retries. Rate limiting (WhatsApp human jitter) is encoded as
-- next_attempt_at on the NEXT pending row — restart-safe, replica-safe.
CREATE TABLE IF NOT EXISTS comm_outbox (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL,
    channel         TEXT NOT NULL CHECK (channel IN ('email', 'whatsapp')),
    recipient       TEXT NOT NULL,
    template        TEXT NOT NULL,
    payload_json    TEXT NOT NULL DEFAULT '{}',
    status          TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sending', 'sent', 'failed', 'dead')),
    attempts        INTEGER NOT NULL DEFAULT 0,
    next_attempt_at DATETIME NOT NULL DEFAULT (datetime('now')),
    last_error      TEXT,
    created_at      DATETIME NOT NULL DEFAULT (datetime('now')),
    sent_at         DATETIME
);

CREATE INDEX IF NOT EXISTS idx_comm_outbox_poll ON comm_outbox(status, next_attempt_at);
CREATE INDEX IF NOT EXISTS idx_comm_outbox_tenant ON comm_outbox(tenant_id, created_at);

-- +goose Down
DROP INDEX IF EXISTS idx_comm_outbox_tenant;
DROP INDEX IF EXISTS idx_comm_outbox_poll;
DROP TABLE IF EXISTS comm_outbox;
DROP INDEX IF EXISTS idx_users_google_sub;
ALTER TABLE users DROP COLUMN phone_verified_at;
ALTER TABLE users DROP COLUMN google_sub;
ALTER TABLE users DROP COLUMN auth_provider;
