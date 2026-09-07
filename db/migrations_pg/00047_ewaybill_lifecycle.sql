-- PG port of 00047_ewaybill_lifecycle.sql | status: PORTABLE | flags: none
-- +goose Up
-- 00047 E-Way Bill lifecycle columns (extends existing eway_bills from 00031)
ALTER TABLE eway_bills ADD COLUMN part_a_json        TEXT;
ALTER TABLE eway_bills ADD COLUMN part_b_json        TEXT;
ALTER TABLE eway_bills ADD COLUMN from_place         TEXT;
ALTER TABLE eway_bills ADD COLUMN from_state_code    TEXT;
ALTER TABLE eway_bills ADD COLUMN to_place           TEXT;
ALTER TABLE eway_bills ADD COLUMN to_state_code      TEXT;
ALTER TABLE eway_bills ADD COLUMN goods_value        REAL;
ALTER TABLE eway_bills ADD COLUMN distance           INTEGER;
ALTER TABLE eway_bills ADD COLUMN doc_type           TEXT;
ALTER TABLE eway_bills ADD COLUMN doc_no             TEXT;
ALTER TABLE eway_bills ADD COLUMN doc_date           TEXT;
ALTER TABLE eway_bills ADD COLUMN transporter_doc_no TEXT;
ALTER TABLE eway_bills ADD COLUMN extension_count    INTEGER DEFAULT 0;
ALTER TABLE eway_bills ADD COLUMN cancel_reason      TEXT;
ALTER TABLE eway_bills ADD COLUMN cancelled_at       TIMESTAMPTZ;
ALTER TABLE eway_bills ADD COLUMN qr_code            TEXT;
ALTER TABLE eway_bills ADD COLUMN ack_no             TEXT;
ALTER TABLE eway_bills ADD COLUMN gen_mode           TEXT DEFAULT 'MANUAL';
-- E-Way Bill event audit trail
CREATE TABLE IF NOT EXISTS eway_bill_events (
    id          TEXT PRIMARY KEY,
    ewb_number  TEXT NOT NULL,
    trip_id     TEXT,
    event_type  TEXT NOT NULL CHECK (event_type IN
        ('PART_A_GENERATED','PART_B_ADDED','VEHICLE_UPDATED','EXTENDED',
         'CANCELLED','EXPIRED','EXTENSION_DENIED','PROVIDER_ERROR','RECOVERED')),
    payload     TEXT,
    created_by  TEXT,
    created_at  TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (ewb_number) REFERENCES eway_bills(ewb_number)
);
CREATE INDEX IF NOT EXISTS idx_ewb_events_ewb ON eway_bill_events(ewb_number);
-- +goose Down
DROP TABLE IF EXISTS eway_bill_events;
ALTER TABLE eway_bills DROP COLUMN part_a_json;
ALTER TABLE eway_bills DROP COLUMN part_b_json;
ALTER TABLE eway_bills DROP COLUMN from_place;
ALTER TABLE eway_bills DROP COLUMN from_state_code;
ALTER TABLE eway_bills DROP COLUMN to_place;
ALTER TABLE eway_bills DROP COLUMN to_state_code;
ALTER TABLE eway_bills DROP COLUMN goods_value;
ALTER TABLE eway_bills DROP COLUMN distance;
ALTER TABLE eway_bills DROP COLUMN doc_type;
ALTER TABLE eway_bills DROP COLUMN doc_no;
ALTER TABLE eway_bills DROP COLUMN doc_date;
ALTER TABLE eway_bills DROP COLUMN transporter_doc_no;
ALTER TABLE eway_bills DROP COLUMN extension_count;
ALTER TABLE eway_bills DROP COLUMN cancel_reason;
ALTER TABLE eway_bills DROP COLUMN cancelled_at;
ALTER TABLE eway_bills DROP COLUMN qr_code;
ALTER TABLE eway_bills DROP COLUMN ack_no;
ALTER TABLE eway_bills DROP COLUMN gen_mode;
