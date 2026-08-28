-- name: CreateFuelPrice :one
INSERT INTO fuel_prices (id, tenant_id, state, city, diesel_price, petrol_price)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING id, tenant_id, state, city, diesel_price, petrol_price, updated_at;

-- name: GetFuelPriceByID :one
SELECT id, tenant_id, state, city, diesel_price, petrol_price, updated_at
FROM fuel_prices WHERE id = ? AND tenant_id = ?;

-- name: GetFuelPriceByLocation :one
SELECT id, tenant_id, state, city, diesel_price, petrol_price, updated_at
FROM fuel_prices
WHERE tenant_id = ? AND state = ? AND city = ?
ORDER BY updated_at DESC LIMIT 1;

-- name: ListFuelPrices :many
SELECT id, tenant_id, state, city, diesel_price, petrol_price, updated_at
FROM fuel_prices
WHERE tenant_id = ?
ORDER BY state, city, updated_at DESC;

-- name: UpdateFuelPrice :one
UPDATE fuel_prices
SET state = ?, city = ?, diesel_price = ?, petrol_price = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND tenant_id = ?
RETURNING id, tenant_id, state, city, diesel_price, petrol_price, updated_at;

-- name: DeleteFuelPrice :exec
DELETE FROM fuel_prices WHERE id = ? AND tenant_id = ?;
