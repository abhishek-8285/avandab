-- +goose Up
-- GST credit/debit notes (post-issuance corrections). Issued invoices are
-- immutable (IRN issued or payments recorded): rate corrections, post-supply
-- discounts and cancellations flow through CREDIT notes; value increases
-- through DEBIT notes. irn is left NULL for notes — NIC supports IRN for
-- CN/DN but e-invoicing of notes is a future integration, out of scope here.
CREATE TABLE IF NOT EXISTS credit_debit_notes (
    id               TEXT PRIMARY KEY,
    tenant_id        TEXT NOT NULL,
    note_number      TEXT NOT NULL UNIQUE,
    note_type        TEXT NOT NULL CHECK (note_type IN ('credit','debit')),
    invoice_id       TEXT NOT NULL REFERENCES invoices(id),
    reason           TEXT NOT NULL,
    place_of_supply  TEXT,
    taxable_value    REAL NOT NULL,
    igst             REAL NOT NULL DEFAULT 0,
    cgst             REAL NOT NULL DEFAULT 0,
    sgst             REAL NOT NULL DEFAULT 0,
    total            REAL NOT NULL,
    irn              TEXT,
    irn_cancelled_at TIMESTAMP,
    created_by       TEXT,
    created_at       TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_credit_debit_notes_tenant_invoice ON credit_debit_notes(tenant_id, invoice_id);

-- Independent per-(financial_year, tenant, note_type) counters. Deliberately
-- separate from invoice_sequences (00048), whose PK omits the prefix: sharing
-- that counter would punch gaps into the GST invoice series.
CREATE TABLE IF NOT EXISTS note_sequences (
    financial_year TEXT NOT NULL,
    tenant_id      TEXT NOT NULL,
    note_type      TEXT NOT NULL,
    last_number    INTEGER NOT NULL DEFAULT 0,
    prefix         TEXT NOT NULL,
    PRIMARY KEY (financial_year, tenant_id, note_type)
);

-- +goose Down
DROP TABLE IF EXISTS note_sequences;
DROP INDEX IF EXISTS idx_credit_debit_notes_tenant_invoice;
DROP TABLE IF EXISTS credit_debit_notes;
