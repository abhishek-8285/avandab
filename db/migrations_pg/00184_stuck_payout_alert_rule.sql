-- +goose Up
-- PG port of 00184_stuck_payout_alert_rule.sql
-- Stuck driver-payout alert rule (see sqlite chain).
INSERT INTO alert_sources (id, name, description) VALUES
 ('src_settlement','settlement','Driver payout / settlement rail failures')
ON CONFLICT (name) DO NOTHING;

INSERT INTO alert_rules (
  id, source, alert_type, name, severity, threshold, threshold_unit,
  cooldown_seconds, storm_window_seconds, storm_batch_min,
  channel_routing, escalation_schedule
) VALUES (
  'rule_payout_stuck','settlement','payout_stuck','Driver Payout Stuck in Processing','critical',NULL,NULL,
  3600,60,1,
  '{"in_app":true}',
  '[{"after_seconds":300,"target_role":"dispatcher","channel":"in_app"},{"after_seconds":900,"target_role":"admin","channel":"in_app"},{"after_seconds":1800,"target_role":"admin","channel":"telegram"}]'
)
ON CONFLICT (id) DO NOTHING;

-- +goose Down
DELETE FROM alert_rules WHERE id = 'rule_payout_stuck';
DELETE FROM alert_sources WHERE name = 'settlement';
