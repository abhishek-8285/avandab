-- name: CreateTrip :one
INSERT INTO trips (id, trip_number, booking_id, driver_id, vehicle_id, route_id,
    departure_time, arrival_time, status, remarks, tenant_id, version,
    started_at, reached_pickup_at, in_transit_at, delivered_at, completed_at, idempotency_key)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?, ?, ?)
RETURNING id, trip_number, booking_id, driver_id, vehicle_id, route_id,
    departure_time, arrival_time, status, remarks, tenant_id, version, created_at, updated_at,
    started_at, reached_pickup_at, in_transit_at, delivered_at, completed_at, idempotency_key;

-- name: GetTripByID :one
SELECT t.id, t.trip_number, t.booking_id, t.driver_id, t.vehicle_id, t.route_id,
    t.departure_time, t.arrival_time, t.status, t.remarks, t.tenant_id, t.version, t.created_at, t.updated_at,
    t.started_at, t.reached_pickup_at, t.in_transit_at, t.delivered_at, t.completed_at,
    d.driver_id AS driver_display_id, d.first_name AS driver_first_name, d.last_name AS driver_last_name,
    v.registration_number AS vehicle_registration_number, v.vehicle_number AS vehicle_number,
    r.source AS route_source, r.destination AS route_destination
FROM trips t
LEFT JOIN drivers d ON t.driver_id = d.id
LEFT JOIN vehicles v ON t.vehicle_id = v.id
LEFT JOIN routes r ON t.route_id = r.id
WHERE t.id = ? AND t.tenant_id = ?;

-- name: GetTripByNumber :one
SELECT t.id, t.trip_number, t.booking_id, t.driver_id, t.vehicle_id, t.route_id,
    t.departure_time, t.arrival_time, t.status, t.remarks, t.tenant_id, t.version, t.created_at, t.updated_at,
    t.started_at, t.reached_pickup_at, t.in_transit_at, t.delivered_at, t.completed_at,
    d.driver_id AS driver_display_id, d.first_name AS driver_first_name, d.last_name AS driver_last_name,
    v.registration_number AS vehicle_registration_number, v.vehicle_number AS vehicle_number,
    r.source AS route_source, r.destination AS route_destination
FROM trips t
LEFT JOIN drivers d ON t.driver_id = d.id
LEFT JOIN vehicles v ON t.vehicle_id = v.id
LEFT JOIN routes r ON t.route_id = r.id
WHERE t.trip_number = ? AND t.tenant_id = ?;

-- name: GetTripByBookingID :one
SELECT t.id, t.trip_number, t.booking_id, t.driver_id, t.vehicle_id, t.route_id,
    t.departure_time, t.arrival_time, t.status, t.remarks, t.tenant_id, t.version, t.created_at, t.updated_at,
    t.started_at, t.reached_pickup_at, t.in_transit_at, t.delivered_at, t.completed_at,
    d.driver_id AS driver_display_id, d.first_name AS driver_first_name, d.last_name AS driver_last_name,
    v.registration_number AS vehicle_registration_number, v.vehicle_number AS vehicle_number,
    r.source AS route_source, r.destination AS route_destination
FROM trips t
LEFT JOIN drivers d ON t.driver_id = d.id
LEFT JOIN vehicles v ON t.vehicle_id = v.id
LEFT JOIN routes r ON t.route_id = r.id
WHERE t.booking_id = ? AND t.tenant_id = ?;

-- name: GetTripByIdempotencyKey :one
SELECT t.id, t.trip_number, t.booking_id, t.driver_id, t.vehicle_id, t.route_id,
    t.departure_time, t.arrival_time, t.status, t.remarks, t.tenant_id, t.version, t.created_at, t.updated_at,
    t.started_at, t.reached_pickup_at, t.in_transit_at, t.delivered_at, t.completed_at,
    d.driver_id AS driver_display_id, d.first_name AS driver_first_name, d.last_name AS driver_last_name,
    v.registration_number AS vehicle_registration_number, v.vehicle_number AS vehicle_number,
    r.source AS route_source, r.destination AS route_destination
FROM trips t
LEFT JOIN drivers d ON t.driver_id = d.id
LEFT JOIN vehicles v ON t.vehicle_id = v.id
LEFT JOIN routes r ON t.route_id = r.id
WHERE t.idempotency_key = ? AND t.tenant_id = ?;

-- name: UpdateTrip :one
UPDATE trips
SET trip_number = ?, booking_id = ?, driver_id = ?, vehicle_id = ?, route_id = ?,
    departure_time = ?, arrival_time = ?, status = ?, remarks = ?,
    started_at = ?, reached_pickup_at = ?, in_transit_at = ?, delivered_at = ?, completed_at = ?,
    version = version + 1,
    updated_at = datetime('now')
WHERE id = ? AND tenant_id = ? AND version = ?
RETURNING trip_number, booking_id, driver_id, vehicle_id, route_id,
    departure_time, arrival_time, status, remarks,
    started_at, reached_pickup_at, in_transit_at, delivered_at, completed_at,
    id, tenant_id, version, created_at, updated_at;




-- name: UpdateTripStatus :one
UPDATE trips
SET status = ?, version = version + 1, updated_at = datetime('now')
WHERE id = ? AND tenant_id = ? AND version = ?
RETURNING id, trip_number, booking_id, driver_id, vehicle_id, route_id,
    departure_time, arrival_time, status, remarks, tenant_id, version, created_at, updated_at;

-- name: AssignDriverToTrip :one
UPDATE trips
SET driver_id = ?, version = version + 1, updated_at = datetime('now')
WHERE id = ? AND tenant_id = ? AND version = ?
RETURNING id, trip_number, booking_id, driver_id, vehicle_id, route_id,
    departure_time, arrival_time, status, remarks, tenant_id, version, created_at, updated_at;

-- name: AssignVehicleToTrip :one
UPDATE trips
SET vehicle_id = ?, version = version + 1, updated_at = datetime('now')
WHERE id = ? AND tenant_id = ? AND version = ?
RETURNING id, trip_number, booking_id, driver_id, vehicle_id, route_id,
    departure_time, arrival_time, status, remarks, tenant_id, version, created_at, updated_at;

-- name: DeleteTrip :exec
DELETE FROM trips WHERE id = ? AND tenant_id = ?;

-- name: SearchTrips :many
SELECT t.id, t.trip_number, t.booking_id, t.driver_id, t.vehicle_id, t.route_id,
    t.departure_time, t.arrival_time, t.status, t.remarks, t.tenant_id, t.created_at, t.updated_at,
    t.started_at, t.reached_pickup_at, t.in_transit_at, t.delivered_at, t.completed_at,
    d.driver_id AS driver_display_id, d.first_name AS driver_first_name, d.last_name AS driver_last_name,
    v.registration_number AS vehicle_registration_number, v.vehicle_number AS vehicle_number,
    r.source AS route_source, r.destination AS route_destination
FROM trips t
LEFT JOIN drivers d ON t.driver_id = d.id
LEFT JOIN vehicles v ON t.vehicle_id = v.id
LEFT JOIN routes r ON t.route_id = r.id
WHERE t.tenant_id = sqlc.arg(tenant_id)
  AND (CAST(sqlc.arg(query) AS text) = '' OR t.trip_number LIKE '%' || sqlc.arg(query) || '%' OR d.first_name LIKE '%' || sqlc.arg(query) || '%' OR d.last_name LIKE '%' || sqlc.arg(query) || '%' OR v.registration_number LIKE '%' || sqlc.arg(query) || '%' OR r.source LIKE '%' || sqlc.arg(query) || '%' OR r.destination LIKE '%' || sqlc.arg(query) || '%')
  AND (CAST(sqlc.arg(status) AS text) = '' OR t.status = sqlc.arg(status))
ORDER BY t.departure_time DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountTrips :one
SELECT COUNT(*) AS count
FROM trips t
LEFT JOIN drivers d ON t.driver_id = d.id
LEFT JOIN vehicles v ON t.vehicle_id = v.id
LEFT JOIN routes r ON t.route_id = r.id
WHERE t.tenant_id = sqlc.arg(tenant_id)
  AND (CAST(sqlc.arg(query) AS text) = '' OR t.trip_number LIKE '%' || sqlc.arg(query) || '%' OR d.first_name LIKE '%' || sqlc.arg(query) || '%' OR d.last_name LIKE '%' || sqlc.arg(query) || '%' OR v.registration_number LIKE '%' || sqlc.arg(query) || '%' OR r.source LIKE '%' || sqlc.arg(query) || '%' OR r.destination LIKE '%' || sqlc.arg(query) || '%')
  AND (CAST(sqlc.arg(status) AS text) = '' OR t.status = sqlc.arg(status));

-- name: CheckVehicleConflict :many
SELECT id, trip_number, status, departure_time, arrival_time
FROM trips
WHERE vehicle_id = ? AND tenant_id = ?
  AND status IN ('scheduled', 'assigned', 'started', 'reached_pickup', 'in_transit', 'delivered')
  AND (? = '' OR id != ?);

-- name: CheckDriverConflict :many
SELECT id, trip_number, status, departure_time, arrival_time
FROM trips
WHERE driver_id = ? AND tenant_id = ?
  AND status IN ('scheduled', 'assigned', 'started', 'reached_pickup', 'in_transit', 'delivered')
  AND (? = '' OR id != ?);

-- name: GetTripsByDate :many
SELECT t.id, t.trip_number, t.booking_id, t.driver_id, t.vehicle_id, t.route_id,
    t.departure_time, t.arrival_time, t.status, t.remarks, t.tenant_id, t.created_at, t.updated_at,
    d.driver_id AS driver_display_id, d.first_name AS driver_first_name, d.last_name AS driver_last_name,
    v.registration_number AS vehicle_registration_number, v.vehicle_number AS vehicle_number,
    r.source AS route_source, r.destination AS route_destination
FROM trips t
LEFT JOIN drivers d ON t.driver_id = d.id
LEFT JOIN vehicles v ON t.vehicle_id = v.id
LEFT JOIN routes r ON t.route_id = r.id
WHERE date(t.departure_time) = CAST(sqlc.arg(departure_date) AS TEXT) AND t.tenant_id = sqlc.arg(tenant_id)
ORDER BY t.departure_time ASC;

-- name: CountTripsByStatus :many
SELECT status, COUNT(*) AS count
FROM trips
WHERE date(departure_time) = CAST(sqlc.arg(departure_date) AS TEXT) AND tenant_id = sqlc.arg(tenant_id)
GROUP BY status;

-- name: GetOverdueTrips :many
SELECT t.id, t.trip_number, t.booking_id, t.driver_id, t.vehicle_id, t.route_id,
    t.departure_time, t.arrival_time, t.status, t.remarks, t.tenant_id, t.created_at, t.updated_at,
    d.driver_id AS driver_display_id, d.first_name AS driver_first_name, d.last_name AS driver_last_name,
    v.registration_number AS vehicle_registration_number, v.vehicle_number AS vehicle_number,
    r.source AS route_source, r.destination AS route_destination
FROM trips t
LEFT JOIN drivers d ON t.driver_id = d.id
LEFT JOIN vehicles v ON t.vehicle_id = v.id
LEFT JOIN routes r ON t.route_id = r.id
WHERE t.tenant_id = ?
  AND t.status IN ('scheduled', 'assigned', 'started', 'reached_pickup', 'in_transit', 'delivered')
  AND t.departure_time < datetime('now')
ORDER BY t.departure_time ASC
LIMIT 10;

-- name: UpdateTripTimeline :one
UPDATE trips
SET started_at = ?, reached_pickup_at = ?, in_transit_at = ?, delivered_at = ?, completed_at = ?,
    status = ?, version = version + 1, updated_at = datetime('now')
WHERE id = ? AND tenant_id = ? AND version = ?
RETURNING id, trip_number, booking_id, driver_id, vehicle_id, route_id,
    departure_time, arrival_time, status, remarks, tenant_id, version, created_at, updated_at,
    started_at, reached_pickup_at, in_transit_at, delivered_at, completed_at;
