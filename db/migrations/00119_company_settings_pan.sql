-- +goose Up
-- 00119: 3-Tier Inclusive & Legal Operator Classification - Add PAN number to company_settings
ALTER TABLE company_settings ADD COLUMN pan_number TEXT;

-- +goose Down
ALTER TABLE company_settings DROP COLUMN pan_number;
