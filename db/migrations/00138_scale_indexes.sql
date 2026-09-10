-- +goose Up
-- 00138: High-scale performance indexes for outbox relay, alerts snooze sweep, and pending fuel audits
CREATE INDEX IF NOT EXISTS idx_outbox_unpublished ON outbox_events(published_at) WHERE published_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_alerts_snooze_status ON alerts(ack_status, snoozed_until) WHERE ack_status = 'snoozed';
CREATE INDEX IF NOT EXISTS idx_driver_expenses_pending_fuel ON driver_expenses(tenant_id, category, audit_status, status) WHERE category = 'fuel' AND audit_status = 'pending';

-- +goose Down
DROP INDEX IF EXISTS idx_driver_expenses_pending_fuel;
DROP INDEX IF EXISTS idx_alerts_snooze_status;
DROP INDEX IF EXISTS idx_outbox_unpublished;
