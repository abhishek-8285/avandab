package sql

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/invoice/domain/aggregate"
	"transport-app/internal/shared"
)

const invoiceSchema = `
CREATE TABLE customers (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	company TEXT
);
CREATE TABLE bookings (
	id TEXT PRIMARY KEY,
	booking_number TEXT
);
CREATE TABLE trips (
	id TEXT PRIMARY KEY,
	trip_number TEXT
);
CREATE TABLE invoices (
	id TEXT PRIMARY KEY,
	invoice_number TEXT NOT NULL UNIQUE,
	booking_id TEXT NOT NULL,
	customer_id TEXT NOT NULL,
	trip_id TEXT,
	subtotal REAL NOT NULL,
	tax REAL NOT NULL DEFAULT 0.0,
	discount REAL NOT NULL DEFAULT 0.0,
	total REAL NOT NULL,
	payment_status TEXT NOT NULL DEFAULT 'pending',
	paid_amount REAL NOT NULL DEFAULT 0.0,
	status TEXT NOT NULL DEFAULT 'outstanding',
	due_date DATETIME,
	version INTEGER NOT NULL DEFAULT 1,
	tenant_id TEXT NOT NULL DEFAULT '1',
	created_at DATETIME NOT NULL DEFAULT (datetime('now')),
	updated_at DATETIME NOT NULL DEFAULT (datetime('now')),
	cgst REAL NOT NULL DEFAULT 0.0,
	sgst REAL NOT NULL DEFAULT 0.0,
	igst REAL NOT NULL DEFAULT 0.0,
	irn TEXT,
	irn_ack_no TEXT,
	irn_ack_date TEXT,
	signed_qr TEXT,
	ewb_number TEXT,
	irn_cancelled_at TIMESTAMP,
	FOREIGN KEY (booking_id) REFERENCES bookings(id),
	FOREIGN KEY (customer_id) REFERENCES customers(id),
	FOREIGN KEY (trip_id) REFERENCES trips(id)
);
CREATE TABLE outbox_events (
	id TEXT PRIMARY KEY,
	aggregate_id TEXT NOT NULL,
	aggregate_type TEXT NOT NULL,
	event_type TEXT NOT NULL,
	payload TEXT NOT NULL,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	published_at DATETIME
);
CREATE TABLE invoice_line_items (
	id          TEXT PRIMARY KEY,
	tenant_id   TEXT NOT NULL DEFAULT '1',
	invoice_id  TEXT NOT NULL,
	trip_id     TEXT,
	line_type   TEXT NOT NULL CHECK (line_type IN ('freight','detention','accessorial')),
	hsn_sac_code TEXT,
	description TEXT NOT NULL,
	unit        TEXT,
	quantity    REAL NOT NULL DEFAULT 1,
	unit_price  REAL NOT NULL DEFAULT 0,
	rate        REAL NOT NULL DEFAULT 0,
	taxable_value REAL NOT NULL DEFAULT 0,
	cgst_rate   REAL NOT NULL DEFAULT 0,
	sgst_rate   REAL NOT NULL DEFAULT 0,
	igst_rate   REAL NOT NULL DEFAULT 0,
	cgst_amount REAL NOT NULL DEFAULT 0,
	sgst_amount REAL NOT NULL DEFAULT 0,
	igst_amount REAL NOT NULL DEFAULT 0,
	amount      REAL NOT NULL DEFAULT 0,
	total       REAL NOT NULL DEFAULT 0,
	ref_id      TEXT,
	created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	FOREIGN KEY (invoice_id) REFERENCES invoices(id),
	FOREIGN KEY (trip_id) REFERENCES trips(id)
);
CREATE INDEX idx_invoice_line_items_invoice ON invoice_line_items(invoice_id);
`

func setupInvoiceDB(t *testing.T) *sql.DB {
	t.Helper()
	dbConn, err := sql.Open("sqlite", ":memory:")
	assert.NoError(t, err)
	_, err = dbConn.Exec(invoiceSchema)
	assert.NoError(t, err)
	_, err = dbConn.Exec(`INSERT INTO customers (id, name, company) VALUES ('cust-1', 'Acme', 'Acme Corp')`)
	assert.NoError(t, err)
	_, err = dbConn.Exec(`INSERT INTO bookings (id, booking_number) VALUES ('bk-1', 'BK-0001')`)
	assert.NoError(t, err)
	return dbConn
}

func TestInvoiceRepository_SaveAndFind(t *testing.T) {
	dbConn := setupInvoiceDB(t)
	defer func() { _ = dbConn.Close() }()

	repo := NewInvoiceRepository(dbConn)
	ctx := context.Background()

	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	agg := aggregate.NewInvoiceAggregate(
		"inv-1",
		shared.TenantID("1"),
		"INV-0001",
		"bk-1",
		"cust-1",
		nil,
		1000.0, 100.0, 0.0, 1100.0,
		aggregate.PaymentStatusPending,
		now,
	)

	err := repo.Save(ctx, agg)
	assert.NoError(t, err)

	found, err := repo.Find(ctx, "inv-1", shared.TenantID("1"))
	assert.NoError(t, err)
	assert.Equal(t, "INV-0001", found.InvoiceNumber)
	assert.Equal(t, 1100.0, found.Total)
	assert.Equal(t, aggregate.PaymentStatusPending, found.PaymentStatus)
	assert.Equal(t, aggregate.InvoiceStatusOutstanding, found.Status)
}

func TestInvoiceRepository_FindByBookingID(t *testing.T) {
	dbConn := setupInvoiceDB(t)
	defer func() { _ = dbConn.Close() }()

	repo := NewInvoiceRepository(dbConn)
	ctx := context.Background()

	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	agg := aggregate.NewInvoiceAggregate(
		"inv-2",
		shared.TenantID("1"),
		"INV-0002",
		"bk-1",
		"cust-1",
		nil,
		500.0, 50.0, 0.0, 550.0,
		aggregate.PaymentStatusPending,
		now,
	)

	err := repo.Save(ctx, agg)
	assert.NoError(t, err)

	found, err := repo.FindByBookingID(ctx, "bk-1", shared.TenantID("1"))
	assert.NoError(t, err)
	assert.Equal(t, "inv-2", string(found.ID))
	assert.Equal(t, "INV-0002", found.InvoiceNumber)
	assert.Equal(t, "bk-1", found.BookingID)
}

func TestInvoiceRepository_SaveUpdatesPaidAmount(t *testing.T) {
	dbConn := setupInvoiceDB(t)
	defer func() { _ = dbConn.Close() }()

	repo := NewInvoiceRepository(dbConn)
	ctx := context.Background()

	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	agg := aggregate.NewInvoiceAggregate(
		"inv-3",
		shared.TenantID("1"),
		"INV-0003",
		"bk-1",
		"cust-1",
		nil,
		1000.0, 100.0, 0.0, 1100.0,
		aggregate.PaymentStatusPending,
		now,
	)

	err := repo.Save(ctx, agg)
	assert.NoError(t, err)

	updated := aggregate.RehydrateInvoiceAggregate(
		"inv-3",
		shared.TenantID("1"),
		"INV-0003",
		"bk-1",
		"cust-1",
		nil,
		1000.0, 100.0, 0.0, 1100.0,
		aggregate.PaymentStatusPaid,
		aggregate.InvoiceStatusPaid,
		500.0, 0.0,
		nil, "", "",
		now, now, 1,
	)
	err = repo.Save(ctx, updated)
	assert.NoError(t, err)

	found, err := repo.Find(ctx, "inv-3", shared.TenantID("1"))
	assert.NoError(t, err)
	assert.Equal(t, 500.0, found.PaidAmount)
	assert.Equal(t, aggregate.InvoiceStatusPaid, found.Status)
	assert.Equal(t, aggregate.PaymentStatusPaid, found.PaymentStatus)
}

func TestInvoiceRepository_FindNonExistent(t *testing.T) {
	dbConn := setupInvoiceDB(t)
	defer func() { _ = dbConn.Close() }()

	repo := NewInvoiceRepository(dbConn)
	ctx := context.Background()

	_, err := repo.Find(ctx, "does-not-exist", shared.TenantID("1"))
	assert.ErrorIs(t, err, sql.ErrNoRows)
}

func TestInvoiceRepository_OptimisticConcurrency(t *testing.T) {
	dbConn := setupInvoiceDB(t)
	defer func() { _ = dbConn.Close() }()

	repo := NewInvoiceRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	agg := aggregate.NewInvoiceAggregate(
		"inv-cc", shared.TenantID("1"), "INV-0099", "bk-1", "cust-1", nil,
		1000.0, 100.0, 0.0, 1100.0, aggregate.PaymentStatusPending, now,
	)
	require.NoError(t, repo.Save(ctx, agg))
	assert.Equal(t, int64(1), agg.Version, "create pins version at 1")

	writerA, err := repo.Find(ctx, "inv-cc", shared.TenantID("1"))
	require.NoError(t, err)
	writerB, err := repo.Find(ctx, "inv-cc", shared.TenantID("1"))
	require.NoError(t, err)
	require.Equal(t, int64(1), writerA.Version)
	require.Equal(t, int64(1), writerB.Version)

	// Writer B commits first.
	writerB.PaidAmount = 300
	writerB.PaymentStatus = aggregate.PaymentStatusPartiallyPaid
	require.NoError(t, repo.Save(ctx, writerB))
	assert.Equal(t, int64(2), writerB.Version, "successful update bumps version")

	// Writer A still holds version 1 — its save must be rejected, not overwrite B.
	writerA.PaidAmount = 999
	err = repo.Save(ctx, writerA)
	require.ErrorIs(t, err, errInvoiceConcurrencyConflict)

	found, err := repo.Find(ctx, "inv-cc", shared.TenantID("1"))
	require.NoError(t, err)
	assert.Equal(t, 300.0, found.PaidAmount, "writer B's data must survive stale write")
	assert.Equal(t, int64(2), found.Version)
}
