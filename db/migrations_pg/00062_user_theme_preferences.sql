-- PG port of 00062_user_theme_preferences.sql | status: PORTABLE | flags: none
-- +goose Up
-- FlyFleet Rule 1 & Spec 12: User Theme Preference (Day / Night / System)
ALTER TABLE users ADD COLUMN theme_preference TEXT NOT NULL DEFAULT 'system' CHECK (theme_preference IN ('light', 'dark', 'system'));
-- +goose Down
ALTER TABLE users DROP COLUMN theme_preference;
