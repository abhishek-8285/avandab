-- name: GetTenantCompanyProfile :one
SELECT * FROM tenant_company_profiles WHERE tenant_id = ?;

-- name: UpsertTenantCompanyProfile :one
INSERT INTO tenant_company_profiles (
    tenant_id, company_name, logo_path, currency, timezone,
    gst_enabled, gst_rate, booking_prefix, trip_prefix, invoice_prefix,
    financial_year, address, phone, email, gst_number, pan_number, state_code,
    updated_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
ON CONFLICT(tenant_id) DO UPDATE SET
    company_name = excluded.company_name,
    logo_path = excluded.logo_path,
    currency = excluded.currency,
    timezone = excluded.timezone,
    gst_enabled = excluded.gst_enabled,
    gst_rate = excluded.gst_rate,
    booking_prefix = excluded.booking_prefix,
    trip_prefix = excluded.trip_prefix,
    invoice_prefix = excluded.invoice_prefix,
    financial_year = excluded.financial_year,
    address = excluded.address,
    phone = excluded.phone,
    email = excluded.email,
    gst_number = excluded.gst_number,
    pan_number = excluded.pan_number,
    state_code = excluded.state_code,
    updated_at = datetime('now')
RETURNING *;
