-- +goose Up
-- 00182: invoice numbers are sequential per tenant, but the column was
-- globally UNIQUE, so two tenants generating INV/2026-27/0001 collided.
-- Rebuild the table with UNIQUE(tenant_id, invoice_number) (SQLite cannot
-- drop an inline UNIQUE constraint).
CREATE TABLE invoices_rebuild_00182 (
    id             TEXT PRIMARY KEY,
    invoice_number TEXT NOT NULL,
    booking_id     TEXT NOT NULL,
    customer_id    TEXT NOT NULL,
    trip_id        TEXT,
    subtotal       REAL NOT NULL,
    tax            REAL NOT NULL DEFAULT 0.0,
    discount       REAL NOT NULL DEFAULT 0.0,
    total          REAL NOT NULL,
    payment_status TEXT NOT NULL DEFAULT 'pending' CHECK (payment_status IN ('pending', 'paid', 'partially_paid')),
    created_at     DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at     DATETIME NOT NULL DEFAULT (datetime('now')),
    tenant_id      TEXT DEFAULT '1' NOT NULL,
    version        INTEGER NOT NULL DEFAULT 1,
    paid_amount    REAL NOT NULL DEFAULT 0.0,
    status         TEXT NOT NULL DEFAULT 'outstanding',
    due_date       DATETIME,
    cgst           REAL NOT NULL DEFAULT 0,
    sgst           REAL NOT NULL DEFAULT 0,
    igst           REAL NOT NULL DEFAULT 0,
    irn            TEXT,
    irn_ack_no     TEXT,
    irn_ack_date   TEXT,
    signed_qr      TEXT,
    ewb_number     TEXT,
    irn_cancelled_at TIMESTAMP,
    FOREIGN KEY (booking_id) REFERENCES bookings(id),
    FOREIGN KEY (customer_id) REFERENCES customers(id),
    FOREIGN KEY (trip_id) REFERENCES trips(id)
);

INSERT INTO invoices_rebuild_00182 SELECT * FROM invoices;
DROP TABLE invoices;
ALTER TABLE invoices_rebuild_00182 RENAME TO invoices;

CREATE UNIQUE INDEX idx_invoices_tenant_number ON invoices(tenant_id, invoice_number);
CREATE INDEX idx_invoices_status ON invoices(status);
CREATE INDEX idx_invoices_due_date ON invoices(due_date);
CREATE UNIQUE INDEX idx_invoices_irn ON invoices(irn) WHERE irn IS NOT NULL;
CREATE INDEX idx_invoices_tenant ON invoices(tenant_id);
CREATE INDEX idx_invoices_tenant_payment_status ON invoices(tenant_id, payment_status);
CREATE UNIQUE INDEX idx_invoices_tenant_trip_unique
    ON invoices(tenant_id, trip_id)
    WHERE trip_id IS NOT NULL AND trip_id != '';

-- +goose StatementBegin
CREATE TRIGGER trg_invoices_tenant_fk_insert
BEFORE INSERT ON invoices
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for invoices.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_invoices_tenant_fk_update
BEFORE UPDATE OF tenant_id ON invoices
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for invoices.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose Down
-- Restore the global UNIQUE constraint. Fails if duplicate numbers already
-- exist across tenants, which is the correct signal: de-duplicate first.
CREATE TABLE invoices_down_00182 (
    id             TEXT PRIMARY KEY,
    invoice_number TEXT NOT NULL UNIQUE,
    booking_id     TEXT NOT NULL,
    customer_id    TEXT NOT NULL,
    trip_id        TEXT,
    subtotal       REAL NOT NULL,
    tax            REAL NOT NULL DEFAULT 0.0,
    discount       REAL NOT NULL DEFAULT 0.0,
    total          REAL NOT NULL,
    payment_status TEXT NOT NULL DEFAULT 'pending' CHECK (payment_status IN ('pending', 'paid', 'partially_paid')),
    created_at     DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at     DATETIME NOT NULL DEFAULT (datetime('now')),
    tenant_id      TEXT DEFAULT '1' NOT NULL,
    version        INTEGER NOT NULL DEFAULT 1,
    paid_amount    REAL NOT NULL DEFAULT 0.0,
    status         TEXT NOT NULL DEFAULT 'outstanding',
    due_date       DATETIME,
    cgst           REAL NOT NULL DEFAULT 0,
    sgst           REAL NOT NULL DEFAULT 0,
    igst           REAL NOT NULL DEFAULT 0,
    irn            TEXT,
    irn_ack_no     TEXT,
    irn_ack_date   TEXT,
    signed_qr      TEXT,
    ewb_number     TEXT,
    irn_cancelled_at TIMESTAMP,
    FOREIGN KEY (booking_id) REFERENCES bookings(id),
    FOREIGN KEY (customer_id) REFERENCES customers(id),
    FOREIGN KEY (trip_id) REFERENCES trips(id)
);

INSERT INTO invoices_down_00182 SELECT * FROM invoices;
DROP TABLE invoices;
ALTER TABLE invoices_down_00182 RENAME TO invoices;

CREATE INDEX idx_invoices_status ON invoices(status);
CREATE INDEX idx_invoices_due_date ON invoices(due_date);
CREATE UNIQUE INDEX idx_invoices_irn ON invoices(irn) WHERE irn IS NOT NULL;
CREATE INDEX idx_invoices_tenant ON invoices(tenant_id);
CREATE INDEX idx_invoices_tenant_payment_status ON invoices(tenant_id, payment_status);
CREATE UNIQUE INDEX idx_invoices_tenant_trip_unique
    ON invoices(tenant_id, trip_id)
    WHERE trip_id IS NOT NULL AND trip_id != '';

-- +goose StatementBegin
CREATE TRIGGER trg_invoices_tenant_fk_insert
BEFORE INSERT ON invoices
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for invoices.tenant_id') END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_invoices_tenant_fk_update
BEFORE UPDATE OF tenant_id ON invoices
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for invoices.tenant_id') END;
END;
-- +goose StatementEnd
