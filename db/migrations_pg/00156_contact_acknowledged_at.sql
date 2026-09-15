-- PG port of 00156_contact_acknowledged_at.sql | status: PORTABLE | flags: none
-- +goose Up
-- SQL in this section is executed when the migration is applied.

ALTER TABLE contact_submissions ADD COLUMN acknowledged_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE contact_submissions DROP COLUMN acknowledged_at;
