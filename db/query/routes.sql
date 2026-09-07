-- name: CreateRoute :one
INSERT INTO routes (
    id, tenant_id, source, destination,
    source_normalized, dest_normalized,
    distance, estimated_hours, standard_fare,
    reverse_distance, reverse_standard_fare,
    direction, is_active, remarks
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id, tenant_id, source, destination, source_normalized, dest_normalized,
          distance, estimated_hours, standard_fare, reverse_distance, reverse_standard_fare,
          direction, is_active, remarks, created_at, updated_at;

-- name: GetRouteByID :one
SELECT id, tenant_id, source, destination, source_normalized, dest_normalized,
       distance, estimated_hours, standard_fare, reverse_distance, reverse_standard_fare,
       direction, is_active, remarks, created_at, updated_at
FROM routes WHERE id = ? AND tenant_id = ?;

-- name: UpdateRoute :one
UPDATE routes
SET source = ?, destination = ?, source_normalized = ?, dest_normalized = ?,
    distance = ?, estimated_hours = ?, standard_fare = ?,
    reverse_distance = ?, reverse_standard_fare = ?,
    direction = ?, is_active = ?, remarks = ?,
    updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND tenant_id = ?
RETURNING id, tenant_id, source, destination, source_normalized, dest_normalized,
          distance, estimated_hours, standard_fare, reverse_distance, reverse_standard_fare,
          direction, is_active, remarks, created_at, updated_at;

-- name: DeleteRoute :exec
DELETE FROM routes WHERE id = ? AND tenant_id = ?;

-- name: SearchRoutes :many
SELECT id, tenant_id, source, destination, source_normalized, dest_normalized,
       distance, estimated_hours, standard_fare, reverse_distance, reverse_standard_fare,
       direction, is_active, remarks, created_at, updated_at
FROM routes
WHERE (lower(source) LIKE '%' || lower(sqlc.arg(search)) || '%' OR lower(destination) LIKE '%' || lower(sqlc.arg(search)) || '%')
  AND tenant_id = sqlc.arg(tenant_id)
ORDER BY created_at DESC
LIMIT sqlc.arg(limit) OFFSET sqlc.arg(offset);

-- name: CountRoutes :one
SELECT COUNT(*) AS count
FROM routes
WHERE (lower(source) LIKE '%' || lower(sqlc.arg(search)) || '%' OR lower(destination) LIKE '%' || lower(sqlc.arg(search)) || '%')
  AND tenant_id = sqlc.arg(tenant_id);

-- name: GetRouteBySourceAndDestination :one
SELECT id, tenant_id, source, destination, source_normalized, dest_normalized,
       distance, estimated_hours, standard_fare, reverse_distance, reverse_standard_fare,
       direction, is_active, remarks, created_at, updated_at
FROM routes
WHERE source_normalized = LOWER(TRIM(:source)) AND dest_normalized = LOWER(TRIM(:destination))
  AND tenant_id = :tenant_id;
