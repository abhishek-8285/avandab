-- name: CreatePlannerRun :one
INSERT INTO planner_runs (
    id, tenant_id, status, job_id, source, kpi_json, created_by
)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING id, tenant_id, status, job_id, source, kpi_json, created_by, created_at, committed_at;

-- name: GetPlannerRun :one
SELECT id, tenant_id, status, job_id, source, kpi_json, created_by, created_at, committed_at
FROM planner_runs
WHERE id = ? AND tenant_id = ?;

-- name: ListPlannerRuns :many
SELECT id, tenant_id, status, job_id, source, kpi_json, created_by, created_at, committed_at
FROM planner_runs
WHERE tenant_id = sqlc.arg(tenant_id)
  AND (sqlc.arg(status_filter) = '' OR status = sqlc.arg(status_filter))
ORDER BY created_at DESC
LIMIT sqlc.arg(limit) OFFSET sqlc.arg(offset);

-- name: CountPlannerRuns :one
SELECT COUNT(*) AS count
FROM planner_runs
WHERE tenant_id = sqlc.arg(tenant_id)
  AND (sqlc.arg(status_filter) = '' OR status = sqlc.arg(status_filter));

-- name: UpdatePlannerRunStatus :one
UPDATE planner_runs
SET status = ?, committed_at = COALESCE(?, committed_at)
WHERE id = ? AND tenant_id = ?
RETURNING id, tenant_id, status, job_id, source, kpi_json, created_by, created_at, committed_at;

-- name: UpdatePlannerRunKpi :one
UPDATE planner_runs
SET kpi_json = ?
WHERE id = ? AND tenant_id = ?
RETURNING id, tenant_id, status, job_id, source, kpi_json, created_by, created_at, committed_at;

-- name: DeletePlannerRun :exec
DELETE FROM planner_runs WHERE id = ? AND tenant_id = ?;

-- name: CreatePlannedRoute :one
INSERT INTO planned_routes (
    id, tenant_id, run_id, seq, vehicle_id, driver_id,
    suggested_vehicle_id, suggested_driver_id, total_km, total_min, toll_cost_est, status
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id, tenant_id, run_id, seq, vehicle_id, driver_id,
          suggested_vehicle_id, suggested_driver_id, total_km, total_min, toll_cost_est,
          status, created_at;

-- name: ListPlannedRoutesByRun :many
SELECT id, tenant_id, run_id, seq, vehicle_id, driver_id,
       suggested_vehicle_id, suggested_driver_id, total_km, total_min, toll_cost_est,
       status, created_at
FROM planned_routes
WHERE run_id = ? AND tenant_id = ?
ORDER BY seq ASC;

-- name: UpdatePlannedRouteAssignment :one
UPDATE planned_routes
SET vehicle_id = ?, driver_id = ?, status = ?
WHERE id = ? AND tenant_id = ?
RETURNING id, tenant_id, run_id, seq, vehicle_id, driver_id,
          suggested_vehicle_id, suggested_driver_id, total_km, total_min, toll_cost_est,
          status, created_at;

-- name: UpdatePlannedRouteMetrics :one
UPDATE planned_routes
SET total_km = ?, total_min = ?, toll_cost_est = ?
WHERE id = ? AND tenant_id = ?
RETURNING id, tenant_id, run_id, seq, vehicle_id, driver_id,
          suggested_vehicle_id, suggested_driver_id, total_km, total_min, toll_cost_est,
          status, created_at;

-- name: UpdatePlannedRouteStatus :one
UPDATE planned_routes
SET status = ?
WHERE id = ? AND tenant_id = ?
RETURNING id, tenant_id, run_id, seq, vehicle_id, driver_id,
          suggested_vehicle_id, suggested_driver_id, total_km, total_min, toll_cost_est,
          status, created_at;

-- name: CreatePlannedStop :one
INSERT INTO planned_stops (
    id, tenant_id, route_id, seq, booking_id, source_type,
    address, lat, lng, time_window_start, time_window_end, demand, skills, status
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id, tenant_id, route_id, seq, booking_id, source_type,
          address, lat, lng, time_window_start, time_window_end, demand, skills,
          status, planned_eta, actual_eta, planned_duration_min, actual_duration_min, created_at;

-- name: ListPlannedStopsByRoute :many
SELECT id, tenant_id, route_id, seq, booking_id, source_type,
       address, lat, lng, time_window_start, time_window_end, demand, skills,
       status, planned_eta, actual_eta, planned_duration_min, actual_duration_min, created_at
FROM planned_stops
WHERE route_id = ? AND tenant_id = ?
ORDER BY seq ASC;

-- name: ListPlannedStopsByRun :many
SELECT s.id, s.tenant_id, s.route_id, s.seq, s.booking_id, s.source_type,
       s.address, s.lat, s.lng, s.time_window_start, s.time_window_end, s.demand, s.skills,
       s.status, s.planned_eta, s.actual_eta, s.planned_duration_min, s.actual_duration_min, s.created_at
FROM planned_stops s
JOIN planned_routes r ON r.id = s.route_id
WHERE r.run_id = ? AND s.tenant_id = ?
ORDER BY r.seq ASC, s.seq ASC;

-- name: UpdatePlannedStopStatus :one
UPDATE planned_stops
SET status = ?
WHERE id = ? AND tenant_id = ?
RETURNING id, tenant_id, route_id, seq, booking_id, source_type,
          address, lat, lng, time_window_start, time_window_end, demand, skills,
          status, planned_eta, actual_eta, planned_duration_min, actual_duration_min, created_at;

-- name: UpdatePlannedStopEta :one
UPDATE planned_stops
SET planned_eta = ?, planned_duration_min = ?
WHERE id = ? AND tenant_id = ?
RETURNING id, tenant_id, route_id, seq, booking_id, source_type,
          address, lat, lng, time_window_start, time_window_end, demand, skills,
          status, planned_eta, actual_eta, planned_duration_min, actual_duration_min, created_at;

-- name: DeletePlannedRoute :exec
DELETE FROM planned_routes WHERE id = ? AND tenant_id = ?;

-- name: CreateDispatchException :one
INSERT INTO dispatch_exceptions (
    id, tenant_id, stop_id, trip_id, vehicle_id, driver_id, kind, status, detail_json
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id, tenant_id, stop_id, trip_id, vehicle_id, driver_id, kind, status, detail_json, created_at, resolved_at;

-- name: ListOpenDispatchExceptions :many
SELECT id, tenant_id, stop_id, trip_id, vehicle_id, driver_id, kind, status, detail_json, created_at, resolved_at
FROM dispatch_exceptions
WHERE tenant_id = ? AND status IN ('open', 'acting')
ORDER BY created_at DESC
LIMIT sqlc.arg(limit) OFFSET sqlc.arg(offset);

-- name: ListDispatchExceptionsByKind :many
SELECT id, tenant_id, stop_id, trip_id, vehicle_id, driver_id, kind, status, detail_json, created_at, resolved_at
FROM dispatch_exceptions
WHERE tenant_id = ? AND kind = ?
  AND (sqlc.arg(status_filter) = '' OR status = sqlc.arg(status_filter))
ORDER BY created_at DESC
LIMIT sqlc.arg(limit) OFFSET sqlc.arg(offset);

-- name: UpdateDispatchExceptionStatus :one
UPDATE dispatch_exceptions
SET status = ?, resolved_at = COALESCE(?, resolved_at)
WHERE id = ? AND tenant_id = ?
RETURNING id, tenant_id, stop_id, trip_id, vehicle_id, driver_id, kind, status, detail_json, created_at, resolved_at;

-- name: CountOpenDispatchExceptions :one
SELECT COUNT(*) AS count
FROM dispatch_exceptions
WHERE tenant_id = ? AND status IN ('open', 'acting');
