-- +goose Up
-- 00160: planned_stops.run_id — PG port of 00160_planned_stops_run_link.sql

ALTER TABLE planned_stops ADD COLUMN run_id TEXT REFERENCES planner_runs(id) ON DELETE CASCADE;

CREATE INDEX IF NOT EXISTS idx_planned_stops_run
    ON planned_stops(tenant_id, run_id) WHERE run_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_planned_stops_run;
ALTER TABLE planned_stops DROP COLUMN IF EXISTS run_id;
