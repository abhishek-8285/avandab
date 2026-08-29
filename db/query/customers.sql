-- name: CreateCustomer :one
INSERT INTO customers (id, customer_code, name, title, company, contact_person, phone, email, gst, address, billing_address, internal_id, photo_url, place_uuid, meta, type, status, payment_terms_days, tenant_id, state_code, notes)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id, customer_code, name, title, company, contact_person, phone, email, gst, address, billing_address, internal_id, photo_url, place_uuid, meta, type, status, payment_terms_days, tenant_id, state_code, notes, created_at, updated_at;

-- name: GetCustomerByID :one
SELECT id, customer_code, name, title, company, contact_person, phone, email, gst, address, billing_address, internal_id, photo_url, place_uuid, meta, type, status, payment_terms_days, tenant_id, state_code, notes, created_at, updated_at
FROM customers WHERE id = ? AND tenant_id = ?;

-- name: UpdateCustomer :one
UPDATE customers
SET customer_code = ?, name = ?, title = ?, company = ?, contact_person = ?, phone = ?, email = ?, gst = ?, address = ?, billing_address = ?, internal_id = ?, photo_url = ?, place_uuid = ?, meta = ?, type = ?, status = ?, payment_terms_days = ?, tenant_id = ?, state_code = ?, notes = ?,
    updated_at = datetime('now')
WHERE id = ?
RETURNING id, customer_code, name, title, company, contact_person, phone, email, gst, address, billing_address, internal_id, photo_url, place_uuid, meta, type, status, payment_terms_days, tenant_id, state_code, notes, created_at, updated_at;

-- name: DeleteCustomer :exec
DELETE FROM customers WHERE id = ? AND tenant_id = ?;

-- name: SearchCustomers :many
SELECT id, customer_code, name, title, company, contact_person, phone, email, gst, address, billing_address, internal_id, photo_url, place_uuid, meta, type, status, payment_terms_days, tenant_id, state_code, notes, created_at, updated_at
FROM customers
WHERE tenant_id = ? AND (customer_code LIKE '%' || ? || '%' OR name LIKE '%' || ? || '%' OR company LIKE '%' || ? || '%' OR phone LIKE '%' || ? || '%' OR email LIKE '%' || ? || '%' OR contact_person LIKE '%' || ? || '%' OR internal_id LIKE '%' || ? || '%')
ORDER BY created_at DESC
LIMIT ? OFFSET ?;

-- name: CountCustomers :one
SELECT COUNT(*) AS count
FROM customers
WHERE tenant_id = ? AND (customer_code LIKE '%' || ? || '%' OR name LIKE '%' || ? || '%' OR company LIKE '%' || ? || '%' OR phone LIKE '%' || ? || '%' OR email LIKE '%' || ? || '%' OR contact_person LIKE '%' || ? || '%' OR internal_id LIKE '%' || ? || '%');

-- name: GetCustomerByPhone :one
SELECT id, customer_code, name, title, company, contact_person, phone, email, gst, address, billing_address, internal_id, photo_url, place_uuid, meta, type, status, payment_terms_days, tenant_id, state_code, notes, created_at, updated_at
FROM customers WHERE phone = ? AND tenant_id = ? LIMIT 1;

-- name: GetCustomerByCode :one
SELECT id, customer_code, name, title, company, contact_person, phone, email, gst, address, billing_address, internal_id, photo_url, place_uuid, meta, type, status, payment_terms_days, tenant_id, state_code, notes, created_at, updated_at
FROM customers WHERE customer_code = ? AND tenant_id = ? LIMIT 1;
