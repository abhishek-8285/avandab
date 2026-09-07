-- PG port of 00022_add_financial_year.sql | status: PORTABLE | flags: none
-- +goose Up
ALTER TABLE company_settings ADD COLUMN financial_year TEXT;
-- +goose Down
ALTER TABLE company_settings DROP COLUMN financial_year;
