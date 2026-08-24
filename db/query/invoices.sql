-- name: CreateInvoice :one
INSERT INTO invoices (id, invoice_number, booking_id, customer_id, trip_id,
    subtotal, tax, discount, total, payment_status, tenant_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id, invoice_number, booking_id, customer_id, trip_id,
    subtotal, tax, discount, total, payment_status, tenant_id, created_at, updated_at;

-- name: GetInvoiceByID :one
SELECT i.id, i.invoice_number, i.booking_id, i.customer_id, i.trip_id,
    i.subtotal, i.tax, i.discount, i.total, i.payment_status, i.tenant_id, i.created_at, i.updated_at,
    c.name AS customer_name, c.company AS customer_company, b.booking_number, t.trip_number
FROM invoices i
JOIN customers c ON i.customer_id = c.id
LEFT JOIN bookings b ON i.booking_id = b.id
LEFT JOIN trips t ON i.trip_id = t.id
WHERE i.id = ? AND i.tenant_id = ?;

-- name: GetInvoiceByNumber :one
SELECT i.id, i.invoice_number, i.booking_id, i.customer_id, i.trip_id,
    i.subtotal, i.tax, i.discount, i.total, i.payment_status, i.tenant_id, i.created_at, i.updated_at,
    c.name AS customer_name, c.company AS customer_company, b.booking_number, t.trip_number
FROM invoices i
JOIN customers c ON i.customer_id = c.id
LEFT JOIN bookings b ON i.booking_id = b.id
LEFT JOIN trips t ON i.trip_id = t.id
WHERE i.invoice_number = ? AND i.tenant_id = ?;

-- name: GetInvoiceByTripID :one
SELECT i.id, i.invoice_number, i.booking_id, i.customer_id, i.trip_id,
    i.subtotal, i.tax, i.discount, i.total, i.payment_status, i.tenant_id, i.created_at, i.updated_at,
    c.name AS customer_name, c.company AS customer_company, b.booking_number, t.trip_number
FROM invoices i
JOIN customers c ON i.customer_id = c.id
LEFT JOIN bookings b ON i.booking_id = b.id
LEFT JOIN trips t ON i.trip_id = t.id
WHERE i.trip_id = ? AND i.tenant_id = ?;

-- name: GetInvoiceByBookingID :one
SELECT i.id, i.invoice_number, i.booking_id, i.customer_id, i.trip_id,
    i.subtotal, i.tax, i.discount, i.total, i.payment_status, i.tenant_id, i.created_at, i.updated_at,
    c.name AS customer_name, c.company AS customer_company, b.booking_number, t.trip_number
FROM invoices i
JOIN customers c ON i.customer_id = c.id
LEFT JOIN bookings b ON i.booking_id = b.id
LEFT JOIN trips t ON i.trip_id = t.id
WHERE i.booking_id = ? AND i.tenant_id = ?;

-- name: UpdateInvoice :one
UPDATE invoices
SET invoice_number = ?, booking_id = ?, customer_id = ?, trip_id = ?,
    subtotal = ?, tax = ?, discount = ?, total = ?, payment_status = ?,
    updated_at = datetime('now')
WHERE id = ? AND tenant_id = ?
RETURNING id, invoice_number, booking_id, customer_id, trip_id,
    subtotal, tax, discount, total, payment_status, tenant_id, created_at, updated_at;

-- name: UpdateInvoicePaymentStatus :one
UPDATE invoices
SET payment_status = ?, updated_at = datetime('now')
WHERE id = ? AND tenant_id = ?
RETURNING id, invoice_number, booking_id, customer_id, trip_id,
    subtotal, tax, discount, total, payment_status, tenant_id, created_at, updated_at;

-- name: DeleteInvoice :exec
DELETE FROM invoices WHERE id = ? AND tenant_id = ?;

-- name: SearchInvoices :many
SELECT i.id, i.invoice_number, i.booking_id, i.customer_id, i.trip_id,
    i.subtotal, i.tax, i.discount, i.total, i.payment_status, i.tenant_id, i.created_at, i.updated_at,
    c.name AS customer_name, c.company AS customer_company, b.booking_number, t.trip_number
FROM invoices i
JOIN customers c ON i.customer_id = c.id
LEFT JOIN bookings b ON i.booking_id = b.id
LEFT JOIN trips t ON i.trip_id = t.id
WHERE i.tenant_id = ?
  AND (i.invoice_number LIKE '%' || ? || '%' OR c.name LIKE '%' || ? || '%')
  AND (? = '' OR i.payment_status = ?)
ORDER BY i.created_at DESC
LIMIT ? OFFSET ?;

-- name: CountInvoices :one
SELECT COUNT(*) AS count
FROM invoices i
JOIN customers c ON i.customer_id = c.id
WHERE i.tenant_id = ?
  AND (i.invoice_number LIKE '%' || ? || '%' OR c.name LIKE '%' || ? || '%')
  AND (? = '' OR i.payment_status = ?);

-- name: GetPendingInvoices :many
SELECT i.id, i.invoice_number, i.booking_id, i.customer_id, i.trip_id,
    i.subtotal, i.tax, i.discount, i.total, i.payment_status, i.tenant_id, i.created_at, i.updated_at,
    c.name AS customer_name, c.company AS customer_company, b.booking_number, t.trip_number
FROM invoices i
JOIN customers c ON i.customer_id = c.id
LEFT JOIN bookings b ON i.booking_id = b.id
LEFT JOIN trips t ON i.trip_id = t.id
WHERE i.payment_status IN ('pending', 'partially_paid') AND i.tenant_id = ?
ORDER BY i.created_at ASC;

-- name: ListInvoicesByCustomer :many
SELECT i.id, i.invoice_number, i.booking_id, i.customer_id, i.trip_id,
    i.subtotal, i.tax, i.discount, i.total, i.payment_status, i.tenant_id, i.created_at, i.updated_at,
    c.name AS customer_name, c.company AS customer_company, b.booking_number, t.trip_number
FROM invoices i
JOIN customers c ON i.customer_id = c.id
LEFT JOIN bookings b ON i.booking_id = b.id
LEFT JOIN trips t ON i.trip_id = t.id
WHERE i.tenant_id = ? AND i.customer_id = ?
ORDER BY i.created_at DESC
LIMIT ?;
