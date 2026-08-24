-- +goose Up
-- Spec 22 §2.9/§10: delivery record for outbound alert channels. One row
-- per send attempt; failures stored with their error, never swallowed.
CREATE TABLE IF NOT EXISTS notification_log (
    id         TEXT PRIMARY KEY,
    channel    TEXT NOT NULL,
    alert_id   TEXT,
    target     TEXT,
    status     TEXT NOT NULL CHECK (status IN ('sent','failed')),
    error      TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_notification_log_alert ON notification_log(alert_id, created_at DESC);
CREATE INDEX idx_notification_log_channel ON notification_log(channel, created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_notification_log_channel;
DROP INDEX IF EXISTS idx_notification_log_alert;
DROP TABLE IF EXISTS notification_log;
