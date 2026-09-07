-- PG port of 00039_experiments.sql | status: PORTABLE | flags: none
-- +goose Up
-- Experiment event logging for the dashboard A/B test (Variant A vs Variant B).
CREATE TABLE IF NOT EXISTS experiment_events (
    id          TEXT PRIMARY KEY,
    tenant_id   TEXT NOT NULL DEFAULT '1',
    user_id     TEXT NOT NULL,
    experiment  TEXT NOT NULL,
    variant     TEXT NOT NULL,
    event       TEXT NOT NULL,
    meta        TEXT NOT NULL DEFAULT '{}',
    created_at  TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_experiment_events_exp ON experiment_events (experiment, created_at);
CREATE INDEX IF NOT EXISTS idx_experiment_events_user ON experiment_events (experiment, user_id, created_at);
-- +goose Down
DROP TABLE IF EXISTS experiment_events;
