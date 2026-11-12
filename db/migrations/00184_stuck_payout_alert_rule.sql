-- +goose Up
-- 00184 — Stuck driver-payout alert rule.
-- The payout rail can leave an instruction in initiated/processing forever when
-- a provider webhook is lost. A leadered sweep publishes payout_stuck events;
-- this rule makes them first-class, escalating alerts.
INSERT OR IGNORE INTO alert_sources (id, name, description) VALUES
 ('src_settlement','settlement','Driver payout / settlement rail failures');

INSERT OR IGNORE INTO alert_rules (
  id, source, alert_type, name, severity, threshold, threshold_unit,
  cooldown_seconds, storm_window_seconds, storm_batch_min,
  channel_routing, escalation_schedule
) VALUES (
  'rule_payout_stuck','settlement','payout_stuck','Driver Payout Stuck in Processing','critical',NULL,NULL,
  3600,60,1,
  '{"in_app":true}',
  '[{"after_seconds":300,"target_role":"dispatcher","channel":"in_app"},{"after_seconds":900,"target_role":"admin","channel":"in_app"},{"after_seconds":1800,"target_role":"admin","channel":"telegram"}]'
);

-- +goose Down
DELETE FROM alert_rules WHERE id = 'rule_payout_stuck';
DELETE FROM alert_sources WHERE name = 'settlement';
