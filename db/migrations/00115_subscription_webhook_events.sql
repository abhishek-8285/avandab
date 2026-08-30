-- +goose Up
-- SQL in this section is executed after the migration is applied.

CREATE TABLE IF NOT EXISTS subscription_webhook_events (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    provider TEXT NOT NULL DEFAULT 'RAZORPAY',
    event_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    provider_subscription_id TEXT NOT NULL,
    event_timestamp DATETIME NOT NULL,
    processed_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    status TEXT NOT NULL DEFAULT 'PROCESSED',
    FOREIGN KEY(tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_sub_webhook_event_provider_id ON subscription_webhook_events(provider, event_id);
CREATE INDEX IF NOT EXISTS idx_sub_webhook_tenant ON subscription_webhook_events(tenant_id);
CREATE INDEX IF NOT EXISTS idx_sub_webhook_sub_time ON subscription_webhook_events(provider_subscription_id, event_timestamp);

-- +goose Down
-- SQL in this section is executed when the migration is rolled back.

DROP TABLE IF EXISTS subscription_webhook_events;
