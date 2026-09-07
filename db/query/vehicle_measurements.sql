-- Measuring points (IK01/IK02/IK03) and measuring documents (IK11).
-- Spec: docs/tech-specs/fleet-registry-sop-parity.md section 4.2.
-- WARNING: keep this file pure ASCII. sqlc v1.31.1 mis-slices statements
-- after non-ASCII bytes (seen: trailing '?' and identifier chars eaten).

-- name: ListMeasuringPointsByVehicle :many
SELECT id, tenant_id, vehicle_id, category, kind, meas_position, unit,
    decimal_places, annual_estimate, count_backwards, is_counter, description, created_at
FROM vehicle_measuring_points
WHERE vehicle_id = ? AND tenant_id = ?
ORDER BY created_at ASC;

-- name: GetMeasuringPointByID :one
SELECT id, tenant_id, vehicle_id, category, kind, meas_position, unit,
    decimal_places, annual_estimate, count_backwards, is_counter, description, created_at
FROM vehicle_measuring_points
WHERE id = ? AND tenant_id = ?;

-- name: GetMeasurementByID :one
SELECT id, tenant_id, point_id, doc_number, counter_reading, difference_reading,
    total_counter_reading, measured_at, read_by, remarks, recorded_at, recorded_by
FROM vehicle_measurements
WHERE id = ? AND tenant_id = ?;

-- name: ListMeasurementsByPoint :many
SELECT id, tenant_id, point_id, doc_number, counter_reading, difference_reading,
    total_counter_reading, measured_at, read_by, remarks, recorded_at, recorded_by
FROM vehicle_measurements
WHERE point_id = ? AND tenant_id = ?
ORDER BY recorded_at DESC
LIMIT ? OFFSET ?;

-- name: GetLastMeasurementByPoint :one
SELECT id, tenant_id, point_id, doc_number, counter_reading, difference_reading,
    total_counter_reading, measured_at, read_by, remarks, recorded_at, recorded_by
FROM vehicle_measurements
WHERE point_id = ? AND tenant_id = ?
ORDER BY recorded_at DESC
LIMIT 1;
