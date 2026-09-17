-- +goose Up
-- 00160: planned_stops.run_id — 00157 links stops to a run only transitively
-- via planned_routes, so unassigned stops (route_id='') have no run owner and
-- two draft runs would cross-contaminate. Adds direct run linkage.
-- Spec: docs/design/dispatcher-route-planner/03-cto-architecture.md §2
-- Append-only per migration ownership rules (never edit 00157).

ALTER TABLE planned_stops ADD COLUMN run_id TEXT;

CREATE INDEX IF NOT EXISTS idx_planned_stops_run
    ON planned_stops(tenant_id, run_id) WHERE run_id IS NOT NULL;

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_planned_stops_run_fk_insert
BEFORE INSERT ON planned_stops
FOR EACH ROW WHEN NEW.run_id IS NOT NULL
BEGIN
  SELECT CASE WHEN NOT EXISTS (
    SELECT 1 FROM planner_runs WHERE id = NEW.run_id AND tenant_id = NEW.tenant_id
  ) THEN RAISE(ABORT, 'FK violation: planner_runs(id) missing for planned_stops.run_id') END;
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS trg_planned_stops_run_fk_update
BEFORE UPDATE OF run_id ON planned_stops
FOR EACH ROW WHEN NEW.run_id IS NOT NULL
BEGIN
  SELECT CASE WHEN NOT EXISTS (
    SELECT 1 FROM planner_runs WHERE id = NEW.run_id AND tenant_id = NEW.tenant_id
  ) THEN RAISE(ABORT, 'FK violation: planner_runs(id) missing for planned_stops.run_id') END;
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS trg_planned_stops_run_fk_update;
DROP TRIGGER IF EXISTS trg_planned_stops_run_fk_insert;
DROP INDEX IF EXISTS idx_planned_stops_run;
ALTER TABLE planned_stops DROP COLUMN run_id;
