-- PG port of 00130_tax_verification_status.sql
-- +goose Up
-- 00130 — GSTIN live-verification seam (mirror): status + verified timestamp.
-- Native ALTER (no rebuild needed on Postgres).
ALTER TABLE tenant_company_profiles ADD COLUMN gstin_verify_status TEXT NOT NULL DEFAULT 'UNVERIFIED'
  CHECK(gstin_verify_status IN ('UNVERIFIED', 'PENDING', 'VERIFIED', 'FAILED'));
ALTER TABLE tenant_company_profiles ADD COLUMN gstin_verified_at TIMESTAMPTZ;
-- +goose Down
ALTER TABLE tenant_company_profiles DROP COLUMN gstin_verified_at;
ALTER TABLE tenant_company_profiles DROP COLUMN gstin_verify_status;
