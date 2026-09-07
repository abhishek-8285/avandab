-- name: CreateBooking :one
INSERT INTO bookings (id, booking_number, customer_id, pickup_date, route_id,
    vehicle_type, passengers, cargo_weight, price, notes, status, tenant_id, version, idempotency_key)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?)
RETURNING id, booking_number, customer_id, pickup_date, route_id, vehicle_type,
    passengers, cargo_weight, price, notes, status, tenant_id, version, created_at, updated_at, idempotency_key;

-- name: GetBookingByIdempotencyKey :one
SELECT b.id, b.booking_number, b.customer_id, b.pickup_date, b.route_id, b.vehicle_type,
    b.passengers, b.cargo_weight, b.price, b.notes, b.status, b.tenant_id, b.version, b.created_at, b.updated_at,
    c.name AS customer_name, c.company AS customer_company, r.source AS route_source, r.destination AS route_destination
FROM bookings b
JOIN customers c ON b.customer_id = c.id
JOIN routes r ON b.route_id = r.id
WHERE b.idempotency_key = ? AND b.tenant_id = ?;

-- name: GetBookingByID :one
SELECT b.id, b.booking_number, b.customer_id, b.pickup_date, b.route_id, b.vehicle_type,
    b.passengers, b.cargo_weight, b.price, b.notes, b.status, b.tenant_id, b.version, b.created_at, b.updated_at,
    c.name AS customer_name, c.company AS customer_company, r.source AS route_source, r.destination AS route_destination
FROM bookings b
JOIN customers c ON b.customer_id = c.id
JOIN routes r ON b.route_id = r.id
WHERE b.id = ? AND b.tenant_id = ?;

-- name: GetBookingByNumber :one
SELECT b.id, b.booking_number, b.customer_id, b.pickup_date, b.route_id, b.vehicle_type,
    b.passengers, b.cargo_weight, b.price, b.notes, b.status, b.tenant_id, b.version, b.created_at, b.updated_at,
    c.name AS customer_name, c.company AS customer_company, r.source AS route_source, r.destination AS route_destination
FROM bookings b
JOIN customers c ON b.customer_id = c.id
JOIN routes r ON b.route_id = r.id
WHERE b.booking_number = ? AND b.tenant_id = ?;

-- name: UpdateBooking :one
UPDATE bookings
SET booking_number = ?, customer_id = ?, pickup_date = ?, route_id = ?, vehicle_type = ?,
    passengers = ?, cargo_weight = ?, price = ?, notes = ?, status = ?,
    version = version + 1,
    updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND tenant_id = ? AND version = ?
RETURNING id, booking_number, customer_id, pickup_date, route_id, vehicle_type,
    passengers, cargo_weight, price, notes, status, tenant_id, version, created_at, updated_at;

-- name: UpdateBookingStatus :one
UPDATE bookings
SET status = ?, version = version + 1, updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND tenant_id = ? AND version = ?
RETURNING id, booking_number, customer_id, pickup_date, route_id, vehicle_type,
    passengers, cargo_weight, price, notes, status, tenant_id, version, created_at, updated_at;

-- name: DeleteBooking :exec
DELETE FROM bookings WHERE id = ? AND tenant_id = ?;

-- name: SearchBookings :many
SELECT b.id, b.booking_number, b.customer_id, b.pickup_date, b.route_id, b.vehicle_type,
    b.passengers, b.cargo_weight, b.price, b.notes, b.status, b.tenant_id, b.created_at, b.updated_at,
    c.name AS customer_name, c.company AS customer_company, r.source AS route_source, r.destination AS route_destination
FROM bookings b
JOIN customers c ON b.customer_id = c.id
JOIN routes r ON b.route_id = r.id
WHERE b.tenant_id = sqlc.arg(tenant_id)
  AND (lower(b.booking_number) LIKE '%' || lower(sqlc.arg(search)) || '%' OR lower(c.name) LIKE '%' || lower(sqlc.arg(search)) || '%' OR lower(c.company) LIKE '%' || lower(sqlc.arg(search)) || '%')
  AND (sqlc.arg(status_all) = '' OR b.status = sqlc.arg(status))
ORDER BY b.pickup_date DESC
LIMIT sqlc.arg(limit) OFFSET sqlc.arg(offset);

-- name: CountBookings :one
SELECT COUNT(*) AS count
FROM bookings b
JOIN customers c ON b.customer_id = c.id
WHERE b.tenant_id = sqlc.arg(tenant_id)
  AND (lower(b.booking_number) LIKE '%' || lower(sqlc.arg(search)) || '%' OR lower(c.name) LIKE '%' || lower(sqlc.arg(search)) || '%' OR lower(c.company) LIKE '%' || lower(sqlc.arg(search)) || '%')
  AND (sqlc.arg(status_all) = '' OR b.status = sqlc.arg(status));

-- name: CountBookingsByDay :many
-- Portable day truncation: substr(CAST(x AS TEXT),1,10) == YYYY-MM-DD on both
-- sqlite (UTC text) and PG (timestamptz text). Lower bound is a Go-side param
-- (impl passes UTC today-29d, matching the old date('now','-29 days')).
SELECT substr(CAST(pickup_date AS TEXT), 1, 10) AS day, COUNT(*) AS count
FROM bookings
WHERE tenant_id = ? AND substr(CAST(pickup_date AS TEXT), 1, 10) >= CAST(sqlc.arg(start_day) AS TEXT)
GROUP BY substr(CAST(pickup_date AS TEXT), 1, 10)
ORDER BY day ASC;

-- name: ListBookingsByCustomer :many
SELECT b.id, b.booking_number, b.customer_id, b.pickup_date, b.route_id, b.vehicle_type,
    b.passengers, b.cargo_weight, b.price, b.notes, b.status, b.tenant_id, b.created_at, b.updated_at,
    c.name AS customer_name, c.company AS customer_company, r.source AS route_source, r.destination AS route_destination
FROM bookings b
JOIN customers c ON b.customer_id = c.id
JOIN routes r ON b.route_id = r.id
WHERE b.tenant_id = ? AND b.customer_id = ?
ORDER BY b.created_at DESC
LIMIT ?;
