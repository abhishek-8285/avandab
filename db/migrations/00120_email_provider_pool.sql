-- +goose Up
-- 00120 — Dynamic email provider pool + quota tracking (zero-cost relay optimization).
-- Enables Brevo 9k/mo + Resend 3k/mo pooling with automatic failover and daily/monthly
-- counters so free tiers are never wasted. Admin can switch primary provider at
-- runtime without restart; counters persist in DB.

-- 1. Provider registry: dynamic overrides for priority/enabled/quotas.
--    Secrets (host/user/password) stay in env; this table only stores routing policy.
CREATE TABLE IF NOT EXISTS email_providers (
    provider      TEXT PRIMARY KEY,
    enabled       INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
    priority      INTEGER NOT NULL DEFAULT 100,
    daily_quota   INTEGER NOT NULL DEFAULT 0,  -- 0 = unlimited
    monthly_quota INTEGER NOT NULL DEFAULT 0,  -- 0 = unlimited
    cost_per_1k   REAL    NOT NULL DEFAULT 0, -- for cost-optimized sorting
    host          TEXT,
    port          TEXT,
    from_addr     TEXT,
    created_at    DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at    DATETIME NOT NULL DEFAULT (datetime('now'))
);

-- 2. Send log: audit + quota source of truth.
--    Each successful send inserts a row; daily/monthly counts are derived via
--    date(created_at) / strftime('%Y-%m',created_at) so resets are automatic.
CREATE TABLE IF NOT EXISTS email_send_log (
    id         TEXT PRIMARY KEY,
    provider   TEXT NOT NULL,
    tenant_id  TEXT NOT NULL DEFAULT '1',
    recipient  TEXT NOT NULL,
    template   TEXT NOT NULL DEFAULT 'generic',
    subject    TEXT,
    status     TEXT NOT NULL CHECK (status IN ('sent','failed')),
    error      TEXT,
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_email_send_log_provider_day
    ON email_send_log(provider, status, created_at);
CREATE INDEX IF NOT EXISTS idx_email_send_log_tenant
    ON email_send_log(tenant_id, created_at);

-- 3. Counters cache (optional speed-up for quota checks, kept in sync with log).
--    daily_used / monthly_used are incremented on success; current_day/month
--    drive automatic reset without cron.
CREATE TABLE IF NOT EXISTS email_provider_counters (
    provider      TEXT PRIMARY KEY,
    daily_used    INTEGER NOT NULL DEFAULT 0,
    monthly_used  INTEGER NOT NULL DEFAULT 0,
    current_day   TEXT NOT NULL DEFAULT (date('now')),
    current_month TEXT NOT NULL DEFAULT (strftime('%Y-%m','now')),
    updated_at    DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (provider) REFERENCES email_providers(provider) ON DELETE CASCADE
);

-- 4. Add provider audit column to comm_outbox so worker can record which
--    relay actually delivered the row (nullable for backward compat).
ALTER TABLE comm_outbox ADD COLUMN provider TEXT;

-- Seed default free-tier providers so the pool is usable with zero env wiring.
-- Quotas match Brevo (300/day, 9000/mo) and Resend (100/day, 3000/mo) free tiers.
INSERT OR IGNORE INTO email_providers (provider, enabled, priority, daily_quota, monthly_quota, cost_per_1k, host, port, from_addr) VALUES
    ('brevo',  1, 1, 300,  9000, 0, 'smtp-relay.brevo.com', '587', 'billing@avandab.com'),
    ('resend', 1, 2, 100,  3000, 0, 'smtp.resend.com',      '587', 'billing@avandab.com'),
    ('direct', 1, 90, 0, 0, 0, 'direct', '', 'billing@avandab.com'),
    ('primary',1, 10, 0, 0, 0, '', '', 'billing@avandab.com');

INSERT OR IGNORE INTO email_provider_counters (provider, daily_used, monthly_used, current_day, current_month) VALUES
    ('brevo',  0, 0, date('now'), strftime('%Y-%m','now')),
    ('resend', 0, 0, date('now'), strftime('%Y-%m','now')),
    ('direct', 0, 0, date('now'), strftime('%Y-%m','now')),
    ('primary',0, 0, date('now'), strftime('%Y-%m','now'));

-- +goose Down
ALTER TABLE comm_outbox DROP COLUMN provider;
DROP INDEX IF EXISTS idx_email_send_log_tenant;
DROP INDEX IF EXISTS idx_email_send_log_provider_day;
DROP TABLE IF EXISTS email_send_log;
DROP TABLE IF EXISTS email_provider_counters;
DROP TABLE IF EXISTS email_providers;
