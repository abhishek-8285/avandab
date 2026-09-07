-- name: CreatePayment :one
INSERT INTO payments (id, invoice_id, payment_date, amount, method, reference, remarks, tenant_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id, invoice_id, payment_date, amount, method, reference, remarks, tenant_id, created_at, updated_at;

-- name: GetPaymentByID :one
SELECT p.id, p.invoice_id, p.payment_date, p.amount, p.method, p.reference, p.remarks, p.tenant_id, p.created_at, p.updated_at,
       i.invoice_number, i.total AS invoice_total, i.payment_status AS invoice_payment_status
FROM payments p
JOIN invoices i ON p.invoice_id = i.id
WHERE p.id = ? AND p.tenant_id = ?;

-- name: SearchPayments :many
SELECT p.id, p.invoice_id, p.payment_date, p.amount, p.method, p.reference, p.remarks, p.tenant_id, p.created_at, p.updated_at,
       i.invoice_number, i.total AS invoice_total, i.payment_status AS invoice_payment_status
FROM payments p
JOIN invoices i ON p.invoice_id = i.id
WHERE p.tenant_id = ? AND (? = '' OR p.method = ?)
ORDER BY p.payment_date DESC
LIMIT ? OFFSET ?;

-- name: CountPayments :one
SELECT COUNT(*) AS count
FROM payments p
JOIN invoices i ON p.invoice_id = i.id
WHERE p.tenant_id = ? AND (? = '' OR p.method = ?);

-- name: GetPaymentsByInvoice :many
SELECT p.id, p.invoice_id, p.payment_date, p.amount, p.method, p.reference, p.remarks, p.tenant_id, p.created_at, p.updated_at,
       i.invoice_number
FROM payments p
JOIN invoices i ON p.invoice_id = i.id
WHERE p.invoice_id = ? AND p.tenant_id = ?
ORDER BY p.payment_date ASC;

-- name: SumPaymentsByInvoice :one
SELECT CAST(COALESCE(SUM(amount), 0) AS REAL) AS total_paid
FROM payments
WHERE invoice_id = ? AND tenant_id = ?;

-- name: DeletePayment :exec
DELETE FROM payments WHERE id = ? AND tenant_id = ?;

-- name: GetTotalRevenue :one
SELECT CAST(COALESCE(SUM(amount), 0) AS REAL) AS total
FROM payments
WHERE tenant_id = ?;

-- name: GetMonthlyRevenue :many
SELECT substr(CAST(payment_date AS TEXT), 1, 7) AS month, CAST(COALESCE(SUM(amount), 0) AS REAL) AS total
FROM payments
WHERE tenant_id = ?
GROUP BY substr(CAST(payment_date AS TEXT), 1, 7)
ORDER BY month ASC;

-- name: GetRevenueByDay :many
-- Lower bound is a Go-side param (impl passes UTC today-29d).
SELECT substr(CAST(payment_date AS TEXT), 1, 10) AS day, CAST(COALESCE(SUM(amount), 0) AS REAL) AS total
FROM payments
WHERE tenant_id = ? AND substr(CAST(payment_date AS TEXT), 1, 10) >= CAST(sqlc.arg(start_day) AS TEXT)
GROUP BY substr(CAST(payment_date AS TEXT), 1, 10)
ORDER BY day ASC;

-- name: GetPaymentsByCustomer :many
SELECT p.id, p.invoice_id, p.payment_date, p.amount, p.method, p.reference, p.remarks, p.tenant_id, p.created_at, p.updated_at,
       i.invoice_number, c.name AS customer_name
FROM payments p
JOIN invoices i ON p.invoice_id = i.id
JOIN customers c ON i.customer_id = c.id
WHERE c.id = ? AND p.tenant_id = ?
ORDER BY p.payment_date DESC
LIMIT ? OFFSET ?;

-- name: CountPaymentsByCustomer :one
SELECT COUNT(*) AS count
FROM payments p
JOIN invoices i ON p.invoice_id = i.id
JOIN customers c ON i.customer_id = c.id
WHERE c.id = ? AND p.tenant_id = ?;
