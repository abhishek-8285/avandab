-- +goose Up
-- 00181 — Default escalation schedules for the shipped alert rules.
-- The escalator has parsed escalation_schedule since 00045, but every seeded
-- rule shipped NULL, so no alert ever escalated. Backfill severity-tiered
-- schedules; any custom schedule is left untouched.
UPDATE alert_rules
SET escalation_schedule = '[{"after_seconds":300,"target_role":"dispatcher","channel":"in_app"},{"after_seconds":900,"target_role":"admin","channel":"in_app"},{"after_seconds":1800,"target_role":"admin","channel":"telegram"}]'
WHERE severity IN ('critical','blocker')
  AND (escalation_schedule IS NULL OR escalation_schedule = '');

UPDATE alert_rules
SET escalation_schedule = '[{"after_seconds":900,"target_role":"dispatcher","channel":"in_app"},{"after_seconds":1800,"target_role":"admin","channel":"in_app"}]'
WHERE severity = 'warning'
  AND (escalation_schedule IS NULL OR escalation_schedule = '');

UPDATE alert_rules
SET escalation_schedule = '[{"after_seconds":1800,"target_role":"admin","channel":"in_app"}]'
WHERE severity = 'info'
  AND (escalation_schedule IS NULL OR escalation_schedule = '');

-- +goose Down
UPDATE alert_rules
SET escalation_schedule = NULL
WHERE escalation_schedule IN (
  '[{"after_seconds":300,"target_role":"dispatcher","channel":"in_app"},{"after_seconds":900,"target_role":"admin","channel":"in_app"},{"after_seconds":1800,"target_role":"admin","channel":"telegram"}]',
  '[{"after_seconds":900,"target_role":"dispatcher","channel":"in_app"},{"after_seconds":1800,"target_role":"admin","channel":"in_app"}]',
  '[{"after_seconds":1800,"target_role":"admin","channel":"in_app"}]'
);
