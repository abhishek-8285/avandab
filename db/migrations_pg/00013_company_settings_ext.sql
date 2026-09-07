-- PG port of 00013_company_settings_ext.sql | status: PORTABLE | flags: none
-- +goose Up
ALTER TABLE company_settings ADD COLUMN address TEXT;
ALTER TABLE company_settings ADD COLUMN phone TEXT;
ALTER TABLE company_settings ADD COLUMN email TEXT;
ALTER TABLE company_settings ADD COLUMN gst_number TEXT;
-- +goose Down
-- SQLite does not easily support dropping columns in older versions, so we recreate the table if necessary.
-- However, for simple rollback we can do nothing or recreate. Since it is dev and modern sqlite, ALTER TABLE DROP COLUMN is supported.
ALTER TABLE company_settings DROP COLUMN address;
ALTER TABLE company_settings DROP COLUMN phone;
ALTER TABLE company_settings DROP COLUMN email;
ALTER TABLE company_settings DROP COLUMN gst_number;
