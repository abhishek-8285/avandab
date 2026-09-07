-- +goose Up
-- PG port of 00128: eway_bills.status CHECK gains part_a + delivered.
-- Native ALTER (no rebuild needed on Postgres).
ALTER TABLE eway_bills DROP CONSTRAINT eway_bills_status_check;
ALTER TABLE eway_bills ADD CONSTRAINT eway_bills_status_check
    CHECK (status IN ('active','part_a','delivered','cancelled','expired'));

-- +goose Down
ALTER TABLE eway_bills DROP CONSTRAINT eway_bills_status_check;
ALTER TABLE eway_bills ADD CONSTRAINT eway_bills_status_check
    CHECK (status IN ('active','cancelled','expired'));
