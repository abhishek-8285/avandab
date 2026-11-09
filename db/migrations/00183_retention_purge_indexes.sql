-- +goose Up
-- 00183 — purge indexes for the business-data retention worker.
-- Telemetry/sessions already have time indexes; these tables previously had
-- no time-leading index, so a retention delete would scan the whole table.
CREATE INDEX IF NOT EXISTS idx_video_events_event_time ON video_safety_events(event_time);
CREATE INDEX IF NOT EXISTS idx_video_livestream_created_at ON video_livestream_sessions(started_at);
CREATE INDEX IF NOT EXISTS idx_breach_incidents_detected_at ON breach_incidents(detected_at);
CREATE INDEX IF NOT EXISTS idx_user_consents_created_at ON user_consents(created_at);
CREATE INDEX IF NOT EXISTS idx_privacy_dsar_created_at ON privacy_dsar_requests(created_at);

-- +goose Down
DROP INDEX IF EXISTS idx_video_events_event_time;
DROP INDEX IF EXISTS idx_video_livestream_created_at;
DROP INDEX IF EXISTS idx_breach_incidents_detected_at;
DROP INDEX IF EXISTS idx_user_consents_created_at;
DROP INDEX IF EXISTS idx_privacy_dsar_created_at;
