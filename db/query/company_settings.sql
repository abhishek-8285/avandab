-- name: GetCompanySettings :one
SELECT * FROM company_settings WHERE id = 1;

-- name: UpdateCompanySettings :one
UPDATE company_settings
SET company_name = ?, logo_path = ?, currency = ?, timezone = ?,
    gst_enabled = ?, gst_rate = ?, booking_prefix = ?, trip_prefix = ?, invoice_prefix = ?,
    financial_year = ?,
    address = ?, phone = ?, email = ?, gst_number = ?, pan_number = ?,
    updated_at = datetime('now')
WHERE id = 1
RETURNING *;

-- name: EnsureCompanySettings :one
INSERT OR IGNORE INTO company_settings (id, company_name, currency, timezone, gst_enabled, gst_rate, booking_prefix, trip_prefix, invoice_prefix, financial_year)
VALUES (1, 'Transport Company', 'INR', 'Asia/Kolkata', 0, 0.0, 'BK', 'TR', 'INV', NULL)
RETURNING *;
