-- +goose Up
-- PG port of 00127: eway_bill_events CHECK gains DELIVERED.
-- Native ALTER (no rebuild needed on Postgres).
ALTER TABLE eway_bill_events DROP CONSTRAINT eway_bill_events_event_type_check;
ALTER TABLE eway_bill_events ADD CONSTRAINT eway_bill_events_event_type_check CHECK (event_type IN
    ('PART_A_GENERATED','PART_B_ADDED','VEHICLE_UPDATED','EXTENDED',
     'CANCELLED','EXPIRED','DELIVERED','EXTENSION_DENIED','PROVIDER_ERROR','RECOVERED'));

-- +goose Down
ALTER TABLE eway_bill_events DROP CONSTRAINT eway_bill_events_event_type_check;
ALTER TABLE eway_bill_events ADD CONSTRAINT eway_bill_events_event_type_check CHECK (event_type IN
    ('PART_A_GENERATED','PART_B_ADDED','VEHICLE_UPDATED','EXTENDED',
     'CANCELLED','EXPIRED','EXTENSION_DENIED','PROVIDER_ERROR','RECOVERED'));
