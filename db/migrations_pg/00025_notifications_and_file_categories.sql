-- PG port of 00025_notifications_and_file_categories.sql | status: PORTABLE | flags: none
-- +goose Up
CREATE TABLE IF NOT EXISTS notifications (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    title TEXT NOT NULL,
    message TEXT NOT NULL,
    channel TEXT NOT NULL DEFAULT 'in_app',
    status TEXT NOT NULL DEFAULT 'unread',
    link TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    read_at TIMESTAMPTZ,
    FOREIGN KEY (user_id) REFERENCES users(id)
);
CREATE INDEX IF NOT EXISTS idx_notifications_user_status ON notifications(user_id, status);
ALTER TABLE files ADD COLUMN category TEXT NOT NULL DEFAULT 'general';
-- +goose Down
DROP TABLE IF EXISTS notifications;
