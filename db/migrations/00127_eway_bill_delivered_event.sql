-- +goose Up
-- +goose NO TRANSACTION
-- 00127: eway_bill_events CHECK gains DELIVERED.
-- The TripDeliveredEvent handler (internal/ewaybill/autogenerate.go) inserts
-- event_type='DELIVERED' on every delivery; without this value the CHECK
-- rejects the row on BOTH engines. SQLite cannot ALTER CHECK, so rebuild.
PRAGMA foreign_keys=OFF;
CREATE TABLE eway_bill_events_new (
    id          TEXT PRIMARY KEY,
    ewb_number  TEXT NOT NULL,
    trip_id     TEXT,
    event_type  TEXT NOT NULL CHECK (event_type IN
        ('PART_A_GENERATED','PART_B_ADDED','VEHICLE_UPDATED','EXTENDED',
         'CANCELLED','EXPIRED','DELIVERED','EXTENSION_DENIED','PROVIDER_ERROR','RECOVERED')),
    payload     TEXT,
    created_by  TEXT,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (ewb_number) REFERENCES eway_bills(ewb_number)
);
INSERT INTO eway_bill_events_new (id, ewb_number, trip_id, event_type, payload, created_by, created_at)
    SELECT id, ewb_number, trip_id, event_type, payload, created_by, created_at FROM eway_bill_events;
DROP TABLE eway_bill_events;
ALTER TABLE eway_bill_events_new RENAME TO eway_bill_events;
PRAGMA foreign_keys=OFF;

-- +goose Down
-- +goose NO TRANSACTION
PRAGMA foreign_keys=OFF;
CREATE TABLE eway_bill_events_old (
    id          TEXT PRIMARY KEY,
    ewb_number  TEXT NOT NULL,
    trip_id     TEXT,
    event_type  TEXT NOT NULL CHECK (event_type IN
        ('PART_A_GENERATED','PART_B_ADDED','VEHICLE_UPDATED','EXTENDED',
         'CANCELLED','EXPIRED','EXTENSION_DENIED','PROVIDER_ERROR','RECOVERED')),
    payload     TEXT,
    created_by  TEXT,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (ewb_number) REFERENCES eway_bills(ewb_number)
);
INSERT INTO eway_bill_events_old (id, ewb_number, trip_id, event_type, payload, created_by, created_at)
    SELECT id, ewb_number, trip_id, event_type, payload, created_by, created_at FROM eway_bill_events
    WHERE event_type != 'DELIVERED';
DROP TABLE eway_bill_events;
ALTER TABLE eway_bill_events_old RENAME TO eway_bill_events;
PRAGMA foreign_keys=OFF;
