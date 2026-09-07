-- PG port of 00075_churn_expense_vernacular.sql | status: PORTABLE | flags: none
-- +goose Up
-- 00075_churn_expense_vernacular.sql
-- No DB — mobile bundle + server kharcha already has receipt. This migration seeds i18n key table.
CREATE TABLE IF NOT EXISTS i18n_keys (
    key TEXT PRIMARY KEY,
    en TEXT NOT NULL,
    hi TEXT NOT NULL DEFAULT '',
    ta TEXT NOT NULL DEFAULT '',
    te TEXT NOT NULL DEFAULT '',
    kn TEXT NOT NULL DEFAULT '',
    mr TEXT NOT NULL DEFAULT '',
    gu TEXT NOT NULL DEFAULT ''
);
INSERT INTO i18n_keys (key,en) VALUES ('trip.accept','Accept'),('trip.reject','Reject') ON CONFLICT DO NOTHING;
-- +goose Down
DROP TABLE IF EXISTS i18n_keys;
