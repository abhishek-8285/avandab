-- PG port of 00049_fastag.sql | status: PORTABLE | flags: none
-- +goose Up
-- 00049 FASTag wallet + transactions

CREATE TABLE IF NOT EXISTS fastag_tags (
    id             TEXT PRIMARY KEY,
    tenant_id      TEXT NOT NULL,
    vehicle_id     TEXT,
    tag_id         TEXT UNIQUE NOT NULL,
    vehicle_number TEXT,
    issuer         TEXT,
    tag_class      TEXT,
    balance        REAL NOT NULL DEFAULT 0,
    status         TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','BLOCKED','EXPIRED','CLOSED')),
    last_sync      TIMESTAMPTZ,
    created_at     TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (vehicle_id) REFERENCES vehicles(id)
);
CREATE INDEX IF NOT EXISTS idx_fastag_tags_vehicle ON fastag_tags(vehicle_number);
CREATE INDEX IF NOT EXISTS idx_fastag_tags_tenant ON fastag_tags(tenant_id);
CREATE TABLE IF NOT EXISTS fastag_transactions (
    id            TEXT PRIMARY KEY,
    tenant_id     TEXT NOT NULL,
    tag_id        TEXT NOT NULL,
    vehicle_id    TEXT,
    vehicle_number TEXT,
    trip_id       TEXT,
    plaza_id      TEXT,
    plaza_name    TEXT,
    amount        REAL NOT NULL DEFAULT 0,
    txn_timestamp TIMESTAMPTZ NOT NULL,
    status        TEXT NOT NULL DEFAULT 'SUCCESS' CHECK (status IN ('SUCCESS','FAILURE','PENDING')),
    source        TEXT NOT NULL DEFAULT 'PROVIDER' CHECK (source IN ('PROVIDER','MANUAL','GPS')),
    reconciled    INTEGER NOT NULL DEFAULT 0,
    kharcha_id    TEXT,
    created_at    TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (tag_id) REFERENCES fastag_tags(tag_id),
    FOREIGN KEY (trip_id) REFERENCES trips(id),
    FOREIGN KEY (kharcha_id) REFERENCES driver_expenses(id)
);
CREATE INDEX IF NOT EXISTS idx_fastag_txn_tag ON fastag_transactions(tag_id, txn_timestamp);
CREATE INDEX IF NOT EXISTS idx_fastag_txn_trip ON fastag_transactions(trip_id);
CREATE INDEX IF NOT EXISTS idx_fastag_txn_recon ON fastag_transactions(reconciled, tenant_id);
-- company_config seed (canonical table is 00042)
INSERT INTO company_config (tenant_id, key, value) VALUES
  ('1', 'fastag_merchant_id', ''),
  ('1', 'fastag_provider', 'MOCK') ON CONFLICT DO NOTHING;
-- +goose Down
DELETE FROM company_config WHERE key IN ('fastag_merchant_id','fastag_provider');
DROP TABLE IF EXISTS fastag_transactions;
DROP TABLE IF EXISTS fastag_tags;
