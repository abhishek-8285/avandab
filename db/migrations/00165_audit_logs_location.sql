-- +goose Up
-- 00165: audit_logs.location — coarse login/action location ("Pune, IN").
-- Sourced from Cloudflare CF-IPCountry/CF-IPCity headers via
-- auth.ClientLocation (no client permission, no extra dependency).
-- NULL/"Unknown" stays NULL so /audit-logs keeps rendering "-".

ALTER TABLE audit_logs ADD COLUMN location TEXT;

-- +goose Down
ALTER TABLE audit_logs DROP COLUMN location;
