-- +goose Up
-- +goose NO TRANSACTION
-- 00128: eway_bills.status CHECK gains part_a + delivered.
-- Code writes both: autogenerate.go transitions bills through
-- 'active' -> 'part_a' -> 'delivered', and monitor.go matches
-- status IN ('active','part_a'). The old CHECK ('active','cancelled',
-- 'expired') rejected those writes on BOTH engines (silent delivery
-- failure: the UPDATE errored so the DELIVERED event never fired).
-- SQLite cannot ALTER CHECK, so rebuild.
PRAGMA foreign_keys=OFF;
CREATE TABLE eway_bills_new (
  id TEXT PRIMARY KEY,
  trip_id TEXT UNIQUE,
  ewb_number TEXT UNIQUE NOT NULL,
  irn TEXT UNIQUE,
  generation_date DATETIME NOT NULL,
  valid_until DATETIME NOT NULL,
  transporter_id TEXT,
  vehicle_number TEXT,
  status TEXT DEFAULT 'active' CHECK (status IN ('active','part_a','delivered','cancelled','expired')),
  raw_response TEXT,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP, part_a_json        TEXT, part_b_json        TEXT, from_place         TEXT, from_state_code    TEXT, to_place           TEXT, to_state_code      TEXT, goods_value        REAL, distance           INTEGER, doc_type           TEXT, doc_no             TEXT, doc_date           TEXT, transporter_doc_no TEXT, extension_count    INTEGER DEFAULT 0, cancel_reason      TEXT, cancelled_at       DATETIME, qr_code            TEXT, ack_no             TEXT, gen_mode           TEXT DEFAULT 'MANUAL',
  FOREIGN KEY (trip_id) REFERENCES trips(id)
);
INSERT INTO eway_bills_new SELECT * FROM eway_bills;
DROP TABLE eway_bills;
ALTER TABLE eway_bills_new RENAME TO eway_bills;
PRAGMA foreign_keys=OFF;

-- +goose Down
-- +goose NO TRANSACTION
PRAGMA foreign_keys=OFF;
CREATE TABLE eway_bills_old (
  id TEXT PRIMARY KEY,
  trip_id TEXT UNIQUE,
  ewb_number TEXT UNIQUE NOT NULL,
  irn TEXT UNIQUE,
  generation_date DATETIME NOT NULL,
  valid_until DATETIME NOT NULL,
  transporter_id TEXT,
  vehicle_number TEXT,
  status TEXT DEFAULT 'active' CHECK (status IN ('active','cancelled','expired')),
  raw_response TEXT,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP, part_a_json        TEXT, part_b_json        TEXT, from_place         TEXT, from_state_code    TEXT, to_place           TEXT, to_state_code      TEXT, goods_value        REAL, distance           INTEGER, doc_type           TEXT, doc_no             TEXT, doc_date           TEXT, transporter_doc_no TEXT, extension_count    INTEGER DEFAULT 0, cancel_reason      TEXT, cancelled_at       DATETIME, qr_code            TEXT, ack_no             TEXT, gen_mode           TEXT DEFAULT 'MANUAL',
  FOREIGN KEY (trip_id) REFERENCES trips(id)
);
INSERT INTO eway_bills_old SELECT * FROM eway_bills WHERE status NOT IN ('part_a','delivered');
DROP TABLE eway_bills;
ALTER TABLE eway_bills_old RENAME TO eway_bills;
PRAGMA foreign_keys=OFF;
