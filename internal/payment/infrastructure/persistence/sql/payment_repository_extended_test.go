package sql

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	paymentagg "transport-app/internal/payment/domain/aggregate"
	"transport-app/internal/repository"
	"transport-app/internal/shared"
)

const paymentExtendedSchema = `
CREATE TABLE invoices (
	id TEXT PRIMARY KEY,
	invoice_number TEXT NOT NULL,
	total REAL NOT NULL,
	payment_status TEXT NOT NULL DEFAULT 'pending',
	tenant_id TEXT NOT NULL DEFAULT '1'
);
CREATE TABLE payments (
	id TEXT PRIMARY KEY,
	invoice_id TEXT NOT NULL,
	payment_date DATETIME NOT NULL,
	amount REAL NOT NULL,
	method TEXT NOT NULL CHECK (method IN ('cash', 'upi', 'bank_transfer', 'cheque', 'razorpay')),
	reference TEXT,
	remarks TEXT,
	tenant_id TEXT NOT NULL DEFAULT '1',
	idempotency_key TEXT,
	razorpay_order_id TEXT,
	razorpay_payment_id TEXT,
	razorpay_signature TEXT,
	webhook_event_id TEXT,
	created_at DATETIME NOT NULL DEFAULT (datetime('now')),
	updated_at DATETIME NOT NULL DEFAULT (datetime('now')),
	FOREIGN KEY (invoice_id) REFERENCES invoices(id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX idx_payments_idempotency ON payments(tenant_id, idempotency_key);
CREATE UNIQUE INDEX idx_payments_webhook_event ON payments(tenant_id, webhook_event_id) WHERE webhook_event_id IS NOT NULL;
CREATE UNIQUE INDEX idx_payments_razorpay_payment ON payments(tenant_id, razorpay_payment_id) WHERE razorpay_payment_id IS NOT NULL;
CREATE TABLE outbox_events (
	id TEXT PRIMARY KEY,
	aggregate_id TEXT NOT NULL,
	aggregate_type TEXT NOT NULL,
	event_type TEXT NOT NULL,
	payload TEXT NOT NULL,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	published_at DATETIME
);
`

func setupPaymentDBExtended(t *testing.T) *sql.DB {
	t.Helper()
	safeName := strings.ReplaceAll(t.Name(), "/", "_")
	safeName = strings.ReplaceAll(safeName, " ", "_")
	safeName = strings.ReplaceAll(safeName, "-", "_")
	dsn := "file:" + safeName + "?mode=memory&cache=shared&_pragma=journal_mode(WAL)"
	dbConn, err := sql.Open("sqlite", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = dbConn.Close() })
	_, err = dbConn.Exec(paymentExtendedSchema)
	require.NoError(t, err)
	_, err = dbConn.Exec(`INSERT INTO invoices (id, invoice_number, total, payment_status, tenant_id) VALUES ('inv-1', 'INV-0001', 1100.0, 'pending', '1'), ('inv-2', 'INV-0002', 2000.0, 'pending', '1'), ('inv-3', 'INV-0003', 3000.0, 'pending', '2')`)
	require.NoError(t, err)
	return dbConn
}

func newPaymentAgg(id, tenantID, invoiceID string, paymentDate time.Time, amount float64, method paymentagg.PaymentMethod, ref, remarks *string, now time.Time) *paymentagg.PaymentAggregate {
	return paymentagg.NewPaymentAggregate(paymentagg.PaymentID(id), shared.TenantID(tenantID), invoiceID, paymentDate, amount, method, ref, remarks, now)
}

// ---------------------------------------------------------------------------
// idempotencyKey and isIdempotencyConflict
// ---------------------------------------------------------------------------

func TestIdempotencyKey_Reference(t *testing.T) {
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	ref := "REF123"
	agg := newPaymentAgg("p1", "1", "inv-1", now, 500, paymentagg.PaymentMethodCash, &ref, nil, now)
	assert.Equal(t, "ref:REF123", idempotencyKey(agg))

	// Trim spaces
	refSpace := "  REF123  "
	agg2 := newPaymentAgg("p2", "1", "inv-1", now, 500, paymentagg.PaymentMethodCash, &refSpace, nil, now)
	assert.Equal(t, "ref:REF123", idempotencyKey(agg2))

	// Empty/whitespace reference falls back to inv:amt:date
	emptyRef := "   "
	agg3 := newPaymentAgg("p3", "1", "inv-99", now, 100.50, paymentagg.PaymentMethodCash, &emptyRef, nil, now)
	key := idempotencyKey(agg3)
	assert.Contains(t, key, "inv:inv-99:amt:100.50:d:2026-08-06")

	// Nil reference
	agg4 := newPaymentAgg("p4", "1", "inv-99", now, 100, paymentagg.PaymentMethodCash, nil, nil, now)
	key4 := idempotencyKey(agg4)
	assert.Equal(t, "inv:inv-99:amt:100.00:d:2026-08-06", key4)

	// Amount rounding via Money (e.g., 100.005 -> 100.01)
	agg5 := newPaymentAgg("p5", "1", "inv-99", now, 100.005, paymentagg.PaymentMethodCash, nil, nil, now)
	key5 := idempotencyKey(agg5)
	// FloatToMoney rounds 100.005*100=10000.5 -> 10001 -> 100.01
	assert.Contains(t, key5, "amt:100.01")

	// Different date -> different key
	day2 := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	agg6 := newPaymentAgg("p6", "1", "inv-99", day2, 100, paymentagg.PaymentMethodCash, nil, nil, day2)
	key6 := idempotencyKey(agg6)
	assert.NotEqual(t, key4, key6)
	assert.Contains(t, key6, "d:2026-08-07")
}

func TestIsIdempotencyConflict(t *testing.T) {
	assert.False(t, isIdempotencyConflict(nil))
	assert.False(t, isIdempotencyConflict(assert.AnError))
	assert.True(t, isIdempotencyConflict(sqlErr("UNIQUE constraint failed: payments.idempotency_key")))
	assert.False(t, isIdempotencyConflict(sqlErr("UNIQUE constraint failed: payments.id")))
	assert.False(t, isIdempotencyConflict(sqlErr("UNIQUE constraint failed: something else")))
	assert.True(t, isIdempotencyConflict(sqlErr("UNIQUE constraint failed: payments.tenant_id, payments.idempotency_key")))
}

func sqlErr(msg string) error {
	return &testSQLErr{msg: msg}
}

type testSQLErr struct{ msg string }

func (e *testSQLErr) Error() string { return e.msg }

// ---------------------------------------------------------------------------
// Save extended
// ---------------------------------------------------------------------------

func TestPaymentRepository_Save_WithReferenceAndRemarks(t *testing.T) {
	dbConn := setupPaymentDBExtended(t)
	repo := NewPaymentRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	ref := "REF-EXT-1"
	remarks := "test remarks"
	agg := newPaymentAgg("pay-ext-1", "1", "inv-1", now, 750, paymentagg.PaymentMethodCheque, &ref, &remarks, now)
	require.NoError(t, repo.Save(ctx, agg))
	found, err := repo.Find(ctx, "pay-ext-1", "1")
	require.NoError(t, err)
	require.NotNil(t, found.Reference)
	assert.Equal(t, "REF-EXT-1", *found.Reference)
	require.NotNil(t, found.Remarks)
	assert.Equal(t, "test remarks", *found.Remarks)

	// verify GetReadModel also sees remarks
	rm, err := repo.GetReadModel(ctx, "pay-ext-1", "1")
	require.NoError(t, err)
	require.NotNil(t, rm.Remarks)
	assert.Equal(t, "test remarks", *rm.Remarks)
	require.NotNil(t, rm.Reference)
	assert.Equal(t, "REF-EXT-1", *rm.Reference)
}

func TestPaymentRepository_Save_ImmutableError(t *testing.T) {
	dbConn := setupPaymentDBExtended(t)
	repo := NewPaymentRepository(dbConn)
	ctx := context.Background()
	now := time.Now()
	agg := newPaymentAgg("pay-immut", "1", "inv-1", now, 100, paymentagg.PaymentMethodCash, nil, nil, now)
	require.NoError(t, repo.Save(ctx, agg))

	// Attempt to save same ID again with different reference (but same idempotency key will dedup, so need different invoice to avoid idempotency hit)
	// Use same ID and same invoice but try to update - should return immutable error because GetPaymentByID finds existing row and Save returns error.
	// However idempotencyKey check happens first: if key matches existing, it returns early with p.ID = existingID without attempting update.
	// To trigger immutable path, we need to use a reference that yields same key? Actually Save checks existingID via findIDByIdempotencyKey before GetPaymentByID.
	// If key exists, it returns early, not reaching immutable check.
	// So to reach immutable branch, we need key NOT already existing but id already exists in DB.
	// We can do: create payment with reference "REF-IMMUT", then try to save same id with different reference "REF-IMMUT-2" => keys differ, so first check misses, then GetPaymentByID finds existing row -> error.
	ref1 := "REF-IMMUT"
	agg1 := newPaymentAgg("pay-immut2", "1", "inv-1", now, 200, paymentagg.PaymentMethodCash, &ref1, nil, now)
	require.NoError(t, repo.Save(ctx, agg1))
	// now try same id with different ref
	ref2 := "REF-IMMUT-2"
	agg2 := newPaymentAgg("pay-immut2", "1", "inv-1", now, 200, paymentagg.PaymentMethodCash, &ref2, nil, now)
	err := repo.Save(ctx, agg2)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "immutable")
}

func TestPaymentRepository_Save_EmptyReferenceIdempotencyFallback(t *testing.T) {
	dbConn := setupPaymentDBExtended(t)
	repo := NewPaymentRepository(dbConn)
	ctx := context.Background()
	day := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	// No reference, fallback key inv:inv-1:amt:300.00:d:2026-08-06
	agg1 := newPaymentAgg("pay-fb-1", "1", "inv-1", day, 300, paymentagg.PaymentMethodCash, nil, nil, day)
	require.NoError(t, repo.Save(ctx, agg1))
	agg2 := newPaymentAgg("pay-fb-2", "1", "inv-1", day, 300, paymentagg.PaymentMethodCash, nil, nil, day)
	require.NoError(t, repo.Save(ctx, agg2))
	// second should collapse to first
	assert.Equal(t, agg1.ID, agg2.ID)
	var count int
	err := dbConn.QueryRow(`SELECT COUNT(*) FROM payments WHERE invoice_id='inv-1' AND amount=300`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

// ---------------------------------------------------------------------------
// FindByReference
// ---------------------------------------------------------------------------

func TestPaymentRepository_FindByReference_Success(t *testing.T) {
	dbConn := setupPaymentDBExtended(t)
	repo := NewPaymentRepository(dbConn)
	ctx := context.Background()
	now := time.Now()
	ref := "REF-FIND-1"
	agg := newPaymentAgg("pay-find-1", "1", "inv-1", now, 100, paymentagg.PaymentMethodUPI, &ref, nil, now)
	require.NoError(t, repo.Save(ctx, agg))
	id, err := repo.FindByReference(ctx, "REF-FIND-1", "1")
	require.NoError(t, err)
	assert.Equal(t, paymentagg.PaymentID("pay-find-1"), id)
}

func TestPaymentRepository_FindByReference_Empty(t *testing.T) {
	dbConn := setupPaymentDBExtended(t)
	repo := NewPaymentRepository(dbConn)
	ctx := context.Background()
	_, err := repo.FindByReference(ctx, "", "1")
	require.ErrorIs(t, err, sql.ErrNoRows)
	_, err = repo.FindByReference(ctx, "   ", "1")
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func TestPaymentRepository_FindByReference_NotFound(t *testing.T) {
	dbConn := setupPaymentDBExtended(t)
	repo := NewPaymentRepository(dbConn)
	ctx := context.Background()
	_, err := repo.FindByReference(ctx, "NONEXISTENT", "1")
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func TestPaymentRepository_FindByReference_TenantIsolation(t *testing.T) {
	dbConn := setupPaymentDBExtended(t)
	repo := NewPaymentRepository(dbConn)
	ctx := context.Background()
	now := time.Now()
	ref := "REF-TENANT"
	agg := newPaymentAgg("pay-tenant-1", "1", "inv-1", now, 100, paymentagg.PaymentMethodCash, &ref, nil, now)
	require.NoError(t, repo.Save(ctx, agg))
	_, err := repo.FindByReference(ctx, "REF-TENANT", "2")
	require.ErrorIs(t, err, sql.ErrNoRows)
	id, err := repo.FindByReference(ctx, "REF-TENANT", "1")
	require.NoError(t, err)
	assert.Equal(t, paymentagg.PaymentID("pay-tenant-1"), id)
}

// ---------------------------------------------------------------------------
// Find extended
// ---------------------------------------------------------------------------

func TestPaymentRepository_Find_TenantIsolation(t *testing.T) {
	dbConn := setupPaymentDBExtended(t)
	repo := NewPaymentRepository(dbConn)
	ctx := context.Background()
	now := time.Now()
	agg := newPaymentAgg("pay-iso-1", "1", "inv-1", now, 100, paymentagg.PaymentMethodCash, nil, nil, now)
	require.NoError(t, repo.Save(ctx, agg))
	_, err := repo.Find(ctx, "pay-iso-1", "2")
	require.Error(t, err)
	found, err := repo.Find(ctx, "pay-iso-1", "1")
	require.NoError(t, err)
	assert.Equal(t, "pay-iso-1", string(found.ID))
}

func TestPaymentRepository_Find_ErrorClosedDB(t *testing.T) {
	dbConn := setupPaymentDBExtended(t)
	repo := NewPaymentRepository(dbConn)
	ctx := context.Background()
	now := time.Now()
	agg := newPaymentAgg("pay-close-1", "1", "inv-1", now, 100, paymentagg.PaymentMethodCash, nil, nil, now)
	require.NoError(t, repo.Save(ctx, agg))
	_ = dbConn.Close()
	_, err := repo.Find(ctx, "pay-close-1", "1")
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// GetReadModel
// ---------------------------------------------------------------------------

func TestPaymentRepository_GetReadModel_Success(t *testing.T) {
	dbConn := setupPaymentDBExtended(t)
	repo := NewPaymentRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	ref := "REF-RM-1"
	remarks := "rmk"
	agg := newPaymentAgg("pay-rm-1", "1", "inv-1", now, 555, paymentagg.PaymentMethodBankTransfer, &ref, &remarks, now)
	require.NoError(t, repo.Save(ctx, agg))
	rm, err := repo.GetReadModel(ctx, "pay-rm-1", "1")
	require.NoError(t, err)
	assert.Equal(t, "pay-rm-1", rm.ID)
	assert.Equal(t, "inv-1", rm.InvoiceID)
	assert.Equal(t, "INV-0001", rm.InvoiceNumber)
	assert.Equal(t, 555.0, rm.Amount)
	assert.Equal(t, string(paymentagg.PaymentMethodBankTransfer), rm.Method)
	require.NotNil(t, rm.Reference)
	assert.Equal(t, "REF-RM-1", *rm.Reference)
	require.NotNil(t, rm.Remarks)
	assert.Equal(t, "rmk", *rm.Remarks)
	assert.False(t, rm.CreatedAt.IsZero())
	assert.False(t, rm.UpdatedAt.IsZero())
}

func TestPaymentRepository_GetReadModel_NilFields(t *testing.T) {
	dbConn := setupPaymentDBExtended(t)
	repo := NewPaymentRepository(dbConn)
	ctx := context.Background()
	now := time.Now()
	agg := newPaymentAgg("pay-rm-nil", "1", "inv-1", now, 100, paymentagg.PaymentMethodCash, nil, nil, now)
	require.NoError(t, repo.Save(ctx, agg))
	rm, err := repo.GetReadModel(ctx, "pay-rm-nil", "1")
	require.NoError(t, err)
	assert.Nil(t, rm.Reference)
	assert.Nil(t, rm.Remarks)
}

func TestPaymentRepository_GetReadModel_NotFound(t *testing.T) {
	dbConn := setupPaymentDBExtended(t)
	repo := NewPaymentRepository(dbConn)
	ctx := context.Background()
	_, err := repo.GetReadModel(ctx, "nonexistent", "1")
	require.Error(t, err)
}

func TestPaymentRepository_GetReadModel_TenantIsolation(t *testing.T) {
	dbConn := setupPaymentDBExtended(t)
	repo := NewPaymentRepository(dbConn)
	ctx := context.Background()
	now := time.Now()
	agg := newPaymentAgg("pay-rm-iso", "1", "inv-1", now, 100, paymentagg.PaymentMethodCash, nil, nil, now)
	require.NoError(t, repo.Save(ctx, agg))
	_, err := repo.GetReadModel(ctx, "pay-rm-iso", "2")
	require.Error(t, err)
}

func TestPaymentRepository_GetReadModel_ErrorClosedDB(t *testing.T) {
	dbConn := setupPaymentDBExtended(t)
	repo := NewPaymentRepository(dbConn)
	ctx := context.Background()
	now := time.Now()
	agg := newPaymentAgg("pay-rm-close", "1", "inv-1", now, 100, paymentagg.PaymentMethodCash, nil, nil, now)
	require.NoError(t, repo.Save(ctx, agg))
	_ = dbConn.Close()
	_, err := repo.GetReadModel(ctx, "pay-rm-close", "1")
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// GetPaymentsByInvoice
// ---------------------------------------------------------------------------

func TestPaymentRepository_GetPaymentsByInvoice_Success(t *testing.T) {
	dbConn := setupPaymentDBExtended(t)
	repo := NewPaymentRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)
	// two payments for inv-1, one for inv-2
	agg1 := newPaymentAgg("pay-inv-1a", "1", "inv-1", now, 100, paymentagg.PaymentMethodCash, nil, nil, now)
	agg2 := newPaymentAgg("pay-inv-1b", "1", "inv-1", now.Add(time.Hour), 200, paymentagg.PaymentMethodUPI, nil, nil, now)
	agg3 := newPaymentAgg("pay-inv-2a", "1", "inv-2", now, 300, paymentagg.PaymentMethodRazorpay, nil, nil, now)
	require.NoError(t, repo.Save(ctx, agg1))
	require.NoError(t, repo.Save(ctx, agg2))
	require.NoError(t, repo.Save(ctx, agg3))

	rows, err := repo.GetPaymentsByInvoice(ctx, "inv-1", "1")
	require.NoError(t, err)
	assert.Len(t, rows, 2)
	// ordered by payment_date ASC
	assert.Equal(t, "pay-inv-1a", rows[0].ID)
	assert.Equal(t, "pay-inv-1b", rows[1].ID)
	assert.Equal(t, "INV-0001", rows[0].InvoiceNumber)

	rows2, err := repo.GetPaymentsByInvoice(ctx, "inv-2", "1")
	require.NoError(t, err)
	assert.Len(t, rows2, 1)
	assert.Equal(t, "pay-inv-2a", rows2[0].ID)

	empty, err := repo.GetPaymentsByInvoice(ctx, "inv-1", "2")
	require.NoError(t, err)
	assert.Len(t, empty, 0)
}

func TestPaymentRepository_GetPaymentsByInvoice_WithReferenceRemarks(t *testing.T) {
	dbConn := setupPaymentDBExtended(t)
	repo := NewPaymentRepository(dbConn)
	ctx := context.Background()
	now := time.Now()
	ref := "REF-GET-INV"
	remarks := "note"
	agg := newPaymentAgg("pay-get-inv-ref", "1", "inv-1", now, 123, paymentagg.PaymentMethodCheque, &ref, &remarks, now)
	require.NoError(t, repo.Save(ctx, agg))
	rows, err := repo.GetPaymentsByInvoice(ctx, "inv-1", "1")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.NotNil(t, rows[0].Reference)
	assert.Equal(t, "REF-GET-INV", *rows[0].Reference)
	require.NotNil(t, rows[0].Remarks)
	assert.Equal(t, "note", *rows[0].Remarks)
}

func TestPaymentRepository_GetPaymentsByInvoice_ErrorClosedDB(t *testing.T) {
	dbConn := setupPaymentDBExtended(t)
	repo := NewPaymentRepository(dbConn)
	ctx := context.Background()
	_ = dbConn.Close()
	_, err := repo.GetPaymentsByInvoice(ctx, "inv-1", "1")
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// SearchReadModels
// ---------------------------------------------------------------------------

func TestPaymentRepository_SearchReadModels_PaginationAndFilter(t *testing.T) {
	dbConn := setupPaymentDBExtended(t)
	repo := NewPaymentRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)
	// Create 3 payments for tenant 1: two cash, one upi, staggered dates
	agg1 := newPaymentAgg("pay-srch-1", "1", "inv-1", now.Add(-3*time.Hour), 100, paymentagg.PaymentMethodCash, nil, nil, now)
	agg2 := newPaymentAgg("pay-srch-2", "1", "inv-1", now.Add(-2*time.Hour), 200, paymentagg.PaymentMethodCash, nil, nil, now)
	agg3 := newPaymentAgg("pay-srch-3", "1", "inv-2", now.Add(-1*time.Hour), 300, paymentagg.PaymentMethodUPI, nil, nil, now)
	aggOther := newPaymentAgg("pay-srch-4", "2", "inv-3", now, 400, paymentagg.PaymentMethodCash, nil, nil, now)
	require.NoError(t, repo.Save(ctx, agg1))
	require.NoError(t, repo.Save(ctx, agg2))
	require.NoError(t, repo.Save(ctx, agg3))
	require.NoError(t, repo.Save(ctx, aggOther))

	// No filter, limit 2
	rows, total, err := repo.SearchReadModels(ctx, "1", "", 2, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	require.Len(t, rows, 2)
	// Ordered by payment_date DESC, so most recent first
	assert.Equal(t, "pay-srch-3", rows[0].ID)

	// Second page
	rows, total, err = repo.SearchReadModels(ctx, "1", "", 2, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	require.Len(t, rows, 1)

	// Filter by method cash -> 2
	rows, total, err = repo.SearchReadModels(ctx, "1", "cash", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	require.Len(t, rows, 2)
	for _, r := range rows {
		assert.Equal(t, "cash", r.Method)
	}

	// Filter by upi -> 1
	rows, total, err = repo.SearchReadModels(ctx, "1", "upi", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, rows, 1)
	assert.Equal(t, "pay-srch-3", rows[0].ID)

	// Tenant isolation
	rows, total, err = repo.SearchReadModels(ctx, "2", "", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Equal(t, "pay-srch-4", rows[0].ID)

	// Offset beyond
	rows, total, err = repo.SearchReadModels(ctx, "1", "", 10, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	assert.Len(t, rows, 0)

	// Non-matching method
	rows, total, err = repo.SearchReadModels(ctx, "1", "razorpay", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(0), total)
	assert.Len(t, rows, 0)
}

func TestPaymentRepository_SearchReadModels_ErrorClosedDB(t *testing.T) {
	dbConn := setupPaymentDBExtended(t)
	repo := NewPaymentRepository(dbConn)
	ctx := context.Background()
	_ = dbConn.Close()
	_, _, err := repo.SearchReadModels(ctx, "1", "", 10, 0)
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// Razorpay fields
// ---------------------------------------------------------------------------

func TestPaymentRepository_RazorpayFields(t *testing.T) {
	dbConn := setupPaymentDBExtended(t)
	repo := NewPaymentRepository(dbConn)
	ctx := context.Background()
	now := time.Now()
	agg := newPaymentAgg("pay-rzp-1", "1", "inv-1", now, 1000, paymentagg.PaymentMethodRazorpay, nil, nil, now)
	require.NoError(t, repo.Save(ctx, agg))

	// initially not found
	_, err := repo.ExistsRazorpayPayment(ctx, "1", "pay_razor_123")
	require.ErrorIs(t, err, sql.ErrNoRows)

	// set fields
	require.NoError(t, repo.SetRazorpayFields(ctx, "pay-rzp-1", "1", "order_123", "pay_razor_123", "sig_abc"))

	// now exists
	id, err := repo.ExistsRazorpayPayment(ctx, "1", "pay_razor_123")
	require.NoError(t, err)
	assert.Equal(t, paymentagg.PaymentID("pay-rzp-1"), id)

	// tenant isolation
	_, err = repo.ExistsRazorpayPayment(ctx, "2", "pay_razor_123")
	require.ErrorIs(t, err, sql.ErrNoRows)

	// verify persisted via raw query
	var orderID, payID, sig sql.NullString
	err = dbConn.QueryRow(`SELECT razorpay_order_id, razorpay_payment_id, razorpay_signature FROM payments WHERE id='pay-rzp-1'`).Scan(&orderID, &payID, &sig)
	require.NoError(t, err)
	assert.Equal(t, "order_123", orderID.String)
	assert.Equal(t, "pay_razor_123", payID.String)
	assert.Equal(t, "sig_abc", sig.String)
}

func TestPaymentRepository_ExistsRazorpayPayment_NotFound(t *testing.T) {
	dbConn := setupPaymentDBExtended(t)
	repo := NewPaymentRepository(dbConn)
	ctx := context.Background()
	_, err := repo.ExistsRazorpayPayment(ctx, "1", "nonexistent")
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func TestPaymentRepository_WebhookEvent(t *testing.T) {
	dbConn := setupPaymentDBExtended(t)
	repo := NewPaymentRepository(dbConn)
	ctx := context.Background()
	now := time.Now()
	agg := newPaymentAgg("pay-wh-1", "1", "inv-1", now, 500, paymentagg.PaymentMethodRazorpay, nil, nil, now)
	require.NoError(t, repo.Save(ctx, agg))

	_, err := repo.ExistsWebhookEvent(ctx, "1", "evt_123")
	require.ErrorIs(t, err, sql.ErrNoRows)

	require.NoError(t, repo.SetWebhookEventID(ctx, "pay-wh-1", "1", "evt_123"))

	id, err := repo.ExistsWebhookEvent(ctx, "1", "evt_123")
	require.NoError(t, err)
	assert.Equal(t, paymentagg.PaymentID("pay-wh-1"), id)

	// tenant isolation
	_, err = repo.ExistsWebhookEvent(ctx, "2", "evt_123")
	require.ErrorIs(t, err, sql.ErrNoRows)

	var webhookID sql.NullString
	err = dbConn.QueryRow(`SELECT webhook_event_id FROM payments WHERE id='pay-wh-1'`).Scan(&webhookID)
	require.NoError(t, err)
	assert.Equal(t, "evt_123", webhookID.String)

	// overwrite
	require.NoError(t, repo.SetWebhookEventID(ctx, "pay-wh-1", "1", "evt_456"))
	id, err = repo.ExistsWebhookEvent(ctx, "1", "evt_456")
	require.NoError(t, err)
	assert.Equal(t, paymentagg.PaymentID("pay-wh-1"), id)
}

func TestPaymentRepository_ExistsWebhookEvent_NotFound(t *testing.T) {
	dbConn := setupPaymentDBExtended(t)
	repo := NewPaymentRepository(dbConn)
	ctx := context.Background()
	_, err := repo.ExistsWebhookEvent(ctx, "1", "nope")
	require.ErrorIs(t, err, sql.ErrNoRows)
}

// ---------------------------------------------------------------------------
// Q and exec with Tx
// ---------------------------------------------------------------------------

func TestPaymentRepository_Q_WithTx(t *testing.T) {
	dbConn := setupPaymentDBExtended(t)
	repoI := NewPaymentRepository(dbConn)
	repo, ok := repoI.(*paymentRepository)
	require.True(t, ok)
	ctx := context.Background()
	now := time.Now()
	agg := newPaymentAgg("pay-tx-1", "1", "inv-1", now, 100, paymentagg.PaymentMethodCash, nil, nil, now)
	require.NoError(t, repo.Save(ctx, agg))

	tx, err := dbConn.Begin()
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	txCtx := repository.WithTxInContext(ctx, tx)
	qTx := repo.Q(txCtx)
	require.NotNil(t, qTx)
	qNoTx := repo.Q(ctx)
	require.NotNil(t, qNoTx)
	assert.NotEqual(t, qTx, qNoTx)

	// Find works inside tx
	found, err := repo.Find(txCtx, "pay-tx-1", "1")
	require.NoError(t, err)
	assert.Equal(t, "pay-tx-1", string(found.ID))

	// SetRazorpayFields inside tx should use exec with tx
	require.NoError(t, repo.SetRazorpayFields(txCtx, "pay-tx-1", "1", "order_tx", "pay_tx", "sig_tx"))
	// Rollback should revert
	_ = tx.Rollback()
	var count int
	err = dbConn.QueryRow(`SELECT COUNT(*) FROM payments WHERE razorpay_payment_id='pay_tx'`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	// Commit path
	tx2, err := dbConn.Begin()
	require.NoError(t, err)
	txCtx2 := repository.WithTxInContext(ctx, tx2)
	require.NoError(t, repo.SetRazorpayFields(txCtx2, "pay-tx-1", "1", "order_tx2", "pay_tx2", "sig2"))
	require.NoError(t, tx2.Commit())
	err = dbConn.QueryRow(`SELECT COUNT(*) FROM payments WHERE razorpay_payment_id='pay_tx2'`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// SetWebhookEventID inside tx
	tx3, err := dbConn.Begin()
	require.NoError(t, err)
	txCtx3 := repository.WithTxInContext(ctx, tx3)
	require.NoError(t, repo.SetWebhookEventID(txCtx3, "pay-tx-1", "1", "evt_tx"))
	_ = tx3.Rollback()
	var wh sql.NullString
	err = dbConn.QueryRow(`SELECT webhook_event_id FROM payments WHERE id='pay-tx-1'`).Scan(&wh)
	require.NoError(t, err)
	// still not "evt_tx" because rollback
	assert.NotEqual(t, "evt_tx", wh.String)

	tx4, err := dbConn.Begin()
	require.NoError(t, err)
	txCtx4 := repository.WithTxInContext(ctx, tx4)
	require.NoError(t, repo.SetWebhookEventID(txCtx4, "pay-tx-1", "1", "evt_tx2"))
	require.NoError(t, tx4.Commit())
	err = dbConn.QueryRow(`SELECT webhook_event_id FROM payments WHERE id='pay-tx-1'`).Scan(&wh)
	require.NoError(t, err)
	assert.Equal(t, "evt_tx2", wh.String)
}

func TestPaymentRepository_Save_ErrorClosedDB(t *testing.T) {
	dbConn := setupPaymentDBExtended(t)
	repo := NewPaymentRepository(dbConn)
	ctx := context.Background()
	now := time.Now()
	agg := newPaymentAgg("pay-close", "1", "inv-1", now, 100, paymentagg.PaymentMethodCash, nil, nil, now)
	_ = dbConn.Close()
	err := repo.Save(ctx, agg)
	require.Error(t, err)
}

func TestPaymentRepository_Save_OutboxError(t *testing.T) {
	dbConn := setupPaymentDBExtended(t)
	repo := NewPaymentRepository(dbConn)
	ctx := context.Background()
	now := time.Now()
	agg := newPaymentAgg("pay-outbox-1", "1", "inv-1", now, 100, paymentagg.PaymentMethodCash, nil, nil, now)
	require.NoError(t, repo.Save(ctx, agg))
	// drop outbox to cause SaveEvents to fail on next update/insert
	_, err := dbConn.Exec(`DROP TABLE outbox_events`)
	require.NoError(t, err)
	agg2 := newPaymentAgg("pay-outbox-2", "1", "inv-1", now, 200, paymentagg.PaymentMethodCash, nil, nil, now)
	err = repo.Save(ctx, agg2)
	require.Error(t, err)
}

func TestPaymentRepository_FindByReference_ErrorClosedDB(t *testing.T) {
	dbConn := setupPaymentDBExtended(t)
	repo := NewPaymentRepository(dbConn)
	ctx := context.Background()
	now := time.Now()
	ref := "REF-CLOSE"
	agg := newPaymentAgg("pay-close-ref", "1", "inv-1", now, 100, paymentagg.PaymentMethodCash, &ref, nil, now)
	require.NoError(t, repo.Save(ctx, agg))
	_ = dbConn.Close()
	_, err := repo.FindByReference(ctx, "REF-CLOSE", "1")
	require.Error(t, err)
}
