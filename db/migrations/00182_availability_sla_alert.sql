-- +goose Up
-- 00182 — Platform availability/SLA alert rule.
-- The availability sampler (00180) measures real 30-day uptime; when it drops
-- below the 99.9% target the SLA monitor publishes an availability.sla_breach
-- event per active tenant. This rule makes those events first-class alerts.
INSERT OR IGNORE INTO alert_sources (id, name, description) VALUES
 ('src_availability','availability','Measured platform availability / SLA breaches');

INSERT OR IGNORE INTO alert_rules (
  id, source, alert_type, name, severity, threshold, threshold_unit,
  cooldown_seconds, storm_window_seconds, storm_batch_min,
  channel_routing, escalation_schedule
) VALUES (
  'rule_sla_breach','availability','sla_breach','30-Day Uptime Below SLA Target','critical',99.9,'percent',
  21600,60,3,
  '{"in_app":true}',
  '[{"after_seconds":300,"target_role":"dispatcher","channel":"in_app"},{"after_seconds":900,"target_role":"admin","channel":"in_app"},{"after_seconds":1800,"target_role":"admin","channel":"in_app"}]'
);

-- +goose Down
DELETE FROM alert_rules WHERE id = 'rule_sla_breach';
DELETE FROM alert_sources WHERE name = 'availability';
