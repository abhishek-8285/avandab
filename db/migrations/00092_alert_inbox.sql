-- +goose Up
-- Spec 22 Step 1 (S1) — Alert inbox.
-- NOTE: spec 22 §3 names table "alert_events"; the actual pipeline store is
-- `alerts` (created 00045). This migration targets the real table and adds
-- the tenant column the spec's inbox index requires (alerts had none).
ALTER TABLE alerts ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '1';
UPDATE alerts SET tenant_id = '1' WHERE tenant_id = '';

ALTER TABLE alerts ADD COLUMN ack_status TEXT NOT NULL DEFAULT 'open'
  CHECK (ack_status IN ('open','snoozed','acked','resolved'));
UPDATE alerts SET ack_status = 'acked' WHERE status = 'acknowledged';
UPDATE alerts SET ack_status = 'resolved' WHERE status IN ('resolved', 'closed');

ALTER TABLE alerts ADD COLUMN severity_rank INTEGER NOT NULL DEFAULT 5;
UPDATE alerts SET severity_rank = CASE severity
  WHEN 'blocker' THEN 1
  WHEN 'critical' THEN 2
  WHEN 'warning' THEN 4
  ELSE 5 END;

ALTER TABLE alerts ADD COLUMN money_at_risk REAL NOT NULL DEFAULT 0;
ALTER TABLE alerts ADD COLUMN snoozed_until TIMESTAMP;

CREATE INDEX idx_alerts_inbox
  ON alerts(tenant_id, ack_status, severity_rank, created_at DESC);

-- RBAC seeds (Spec 22 §3): idempotent, reuse existing where present.
INSERT OR IGNORE INTO permissions (name, description) VALUES
  ('alerts:read', 'View ranked alert inbox'),
  ('alerts:write', 'Acknowledge and snooze alerts in the inbox'),
  ('kharcha:approve', 'Approve or reject driver expense claims'),
  ('compliance:read', 'View compliance radar and document expiry'),
  ('driver:read-self', 'Driver self-service: view own balance, settlements, advances'),
  ('driver:write-self', 'Driver self-service: request advances, submit expenses');

INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT 1, id FROM permissions WHERE name IN (
  'alerts:read','alerts:write','kharcha:approve','compliance:read',
  'driver:read-self','driver:write-self');

INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT 2, id FROM permissions WHERE name IN ('alerts:read','alerts:write','compliance:read');

INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT 4, id FROM permissions WHERE name IN ('alerts:read');

-- +goose Down
DROP INDEX IF EXISTS idx_alerts_inbox;
DELETE FROM role_permissions WHERE permission_id IN (
  SELECT id FROM permissions WHERE name IN (
    'alerts:write','kharcha:approve','driver:read-self','driver:write-self'));
DELETE FROM permissions WHERE name IN (
  'alerts:write','kharcha:approve','compliance:read','driver:read-self','driver:write-self');
ALTER TABLE alerts DROP COLUMN snoozed_until;
ALTER TABLE alerts DROP COLUMN money_at_risk;
ALTER TABLE alerts DROP COLUMN severity_rank;
ALTER TABLE alerts DROP COLUMN ack_status;
ALTER TABLE alerts DROP COLUMN tenant_id;
