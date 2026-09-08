-- +goose Up
-- 00130 — GSTIN live-verification seam: verification status lives with the
-- per-tenant company profile (00125). UNVERIFIED default keeps every existing
-- org valid; a future provider worker flips PENDING→VERIFIED/FAILED and stamps
-- verified_at. The offline checksum stays the entry gate; these columns record
-- the live portal lookup, which needs GSP/vendor credentials (not configured).
ALTER TABLE tenant_company_profiles ADD COLUMN gstin_verify_status TEXT NOT NULL DEFAULT 'UNVERIFIED'
  CHECK(gstin_verify_status IN ('UNVERIFIED', 'PENDING', 'VERIFIED', 'FAILED'));
ALTER TABLE tenant_company_profiles ADD COLUMN gstin_verified_at DATETIME;

-- +goose Down
ALTER TABLE tenant_company_profiles DROP COLUMN gstin_verified_at;
ALTER TABLE tenant_company_profiles DROP COLUMN gstin_verify_status;
