-- +goose Up
-- SQL in this section is executed after the migration is applied.

CREATE TABLE IF NOT EXISTS subscription_plans (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    monthly_price_inr NUMERIC(10, 2) NOT NULL DEFAULT 0.00,
    features_json TEXT NOT NULL DEFAULT '[]',
    quotas_json TEXT NOT NULL DEFAULT '{}',
    is_active INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS tenant_subscriptions (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    plan_id TEXT NOT NULL REFERENCES subscription_plans(id),
    status TEXT NOT NULL CHECK(status IN ('TRIAL', 'ACTIVE', 'PAST_DUE', 'GRACE', 'READ_ONLY', 'ACCOUNT_CLOSED', 'OPERATIONALLY_TERMINATED')),
    current_period_start DATETIME NOT NULL,
    current_period_end DATETIME NOT NULL,
    trial_end DATETIME,
    provider_subscription_id TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_tenant_subscriptions_active ON tenant_subscriptions(tenant_id);

CREATE TABLE IF NOT EXISTS tenant_entitlement_overrides (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    entitlement_type TEXT NOT NULL CHECK(entitlement_type IN ('FEATURE', 'QUOTA')),
    key_name TEXT NOT NULL,
    override_value TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    expires_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_tenant_entitlement_override_key ON tenant_entitlement_overrides(tenant_id, entitlement_type, key_name);

CREATE TABLE IF NOT EXISTS tenant_usage_meters (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    quota_key TEXT NOT NULL,
    period_start DATETIME NOT NULL,
    period_end DATETIME NOT NULL,
    used_quantity INTEGER NOT NULL DEFAULT 0,
    reserved_quantity INTEGER NOT NULL DEFAULT 0,
    max_quantity INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_tenant_usage_meters_period ON tenant_usage_meters(tenant_id, quota_key, period_start, period_end);

CREATE TABLE IF NOT EXISTS tenant_usage_events (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    quota_key TEXT NOT NULL,
    quantity INTEGER NOT NULL DEFAULT 1,
    idempotency_key TEXT NOT NULL,
    source_entity_type TEXT NOT NULL,
    source_entity_id TEXT NOT NULL,
    timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_tenant_usage_events_idem ON tenant_usage_events(tenant_id, idempotency_key);
CREATE INDEX IF NOT EXISTS idx_tenant_usage_events_tenant_time ON tenant_usage_events(tenant_id, timestamp);

-- Seed Standard Plans
INSERT OR IGNORE INTO subscription_plans (id, name, description, monthly_price_inr, features_json, quotas_json)
VALUES
('STARTER', 'Starter Trial', 'Free Starter Evaluation Tier', 0.00,
 '["mobile_app_epod", "basic_invoicing", "basic_tracking"]',
 '{"max_vehicles": 3, "max_drivers": 5, "max_trips_per_month": 10, "max_dispatcher_seats": 1}'),

('GROWTH', 'Growth Tier', 'Growing Fleets up to 15 vehicles', 1999.00,
 '["mobile_app_epod", "basic_invoicing", "basic_tracking", "multi_stop", "automated_ewb", "control_tower", "driver_settlements"]',
 '{"max_vehicles": 15, "max_drivers": 25, "max_trips_per_month": 250, "max_dispatcher_seats": 3}'),

('SCALE', 'Scale Fleet', 'Established Commercial Transporters', 4999.00,
 '["mobile_app_epod", "basic_invoicing", "basic_tracking", "multi_stop", "automated_ewb", "control_tower", "driver_settlements", "advanced_analytics", "api_access"]',
 '{"max_vehicles": 1000, "max_drivers": 2000, "max_trips_per_month": 100000, "max_dispatcher_seats": 100}'),

('ENTERPRISE', 'Enterprise Custom', 'Custom Contractual Operations', 0.00,
 '["mobile_app_epod", "basic_invoicing", "basic_tracking", "multi_stop", "automated_ewb", "control_tower", "driver_settlements", "advanced_analytics", "api_access", "dedicated_sla"]',
 '{"max_vehicles": 100000, "max_drivers": 100000, "max_trips_per_month": 1000000, "max_dispatcher_seats": 1000}');

-- +goose Down
-- SQL in this section is executed when the migration is rolled back.

DROP TABLE IF EXISTS tenant_usage_events;
DROP TABLE IF EXISTS tenant_usage_meters;
DROP TABLE IF EXISTS tenant_entitlement_overrides;
DROP TABLE IF EXISTS tenant_subscriptions;
DROP TABLE IF EXISTS subscription_plans;
