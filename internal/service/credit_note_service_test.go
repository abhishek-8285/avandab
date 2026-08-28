package service_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/domain"
	sqliterepo "transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

// insertNoteTestInvoice plants a minimal issued invoice (total 1180) owned by
// tenantID, mirroring the ledger-test fixture shape.
func insertNoteTestInvoice(t *testing.T, db *sql.DB, tenantID, invID string) {
	t.Helper()
	suffix := strings.ReplaceAll(invID, "-", "_")
	_, err := db.Exec(`INSERT INTO customers (id, name, phone) VALUES (?, 'Note Buyer', '+919999999999')`,
		"cus-"+suffix)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO invoices (id, invoice_number, booking_id, customer_id, subtotal, tax, total, payment_status, tenant_id)
		VALUES (?, ?, ?, ?, 1000, 180, 1180, 'pending', ?)`,
		invID, "INV-NOTE-"+strings.ToUpper(suffix), "bk-"+suffix, "cus-"+suffix, tenantID)
	require.NoError(t, err)
}

// TestCreditNote_NumberingSequentialPerType proves {CN|DN}/{FY}/{seq:04d}
// allocation is gap-free AND that credit and debit counters advance
// independently (note_sequences PK includes note_type).
func TestCreditNote_NumberingSequentialPerType(t *testing.T) {
	dbConn, svcs := openMigratedDB(t)
	ctx := shared.ContextWithTenantID(context.Background(), shared.TenantID("tenant-seq"))
	insertNoteTestInvoice(t, dbConn, "tenant-seq", "inv-seq")

	fy := sqliterepo.FinancialYear(time.Now())
	type want struct {
		num string
		typ string
	}
	wantSeq := []want{
		{fmt.Sprintf("CN/%s/0001", fy), "credit"},
		{fmt.Sprintf("CN/%s/0002", fy), "credit"},
		{fmt.Sprintf("DN/%s/0001", fy), "debit"},
		{fmt.Sprintf("CN/%s/0003", fy), "credit"},
		{fmt.Sprintf("DN/%s/0002", fy), "debit"},
	}
	cnCount, dnCount := 0, 0
	for i, w := range wantSeq {
		var (
			note *service.CreditNoteRecord
			err  error
		)
		if w.typ == "credit" {
			cnCount++
			note, err = svcs.Notes.CreateCreditNote(ctx, service.NoteRequest{
				InvoiceID: "inv-seq", Reason: fmt.Sprintf("correction %d", i), TaxableValue: 10,
			})
		} else {
			dnCount++
			note, err = svcs.Notes.CreateDebitNote(ctx, service.NoteRequest{
				InvoiceID: "inv-seq", Reason: fmt.Sprintf("extra charge %d", i), TaxableValue: 10,
			})
		}
		require.NoError(t, err)
		assert.Equal(t, w.num, note.NoteNumber, "note %d must allocate %s", i, w.num)

		var last int64
		require.NoError(t, dbConn.QueryRow(
			`SELECT last_number FROM note_sequences WHERE financial_year=? AND tenant_id='tenant-seq' AND note_type=?`,
			fy, w.typ).Scan(&last))
		expected := int64(cnCount)
		if w.typ == "debit" {
			expected = int64(dnCount)
		}
		assert.Equal(t, expected, last, "%s counter must track only %s notes", w.typ, w.typ)
	}

	var seqRows int
	require.NoError(t, dbConn.QueryRow(
		`SELECT COUNT(*) FROM note_sequences WHERE tenant_id='tenant-seq'`).Scan(&seqRows))
	assert.Equal(t, 2, seqRows, "one sequence row per note_type")
}

// TestDebitNote_NoUpperBound proves debit notes may exceed the invoice value.
func TestDebitNote_NoUpperBound(t *testing.T) {
	dbConn, svcs := openMigratedDB(t)
	ctx := shared.ContextWithTenantID(context.Background(), shared.TenantID("tenant-dn"))
	insertNoteTestInvoice(t, dbConn, "tenant-dn", "inv-dn")

	note, err := svcs.Notes.CreateDebitNote(ctx, service.NoteRequest{
		InvoiceID: "inv-dn", Reason: "late freight surcharge", TaxableValue: 99999,
	})
	require.NoError(t, err)
	assert.Greater(t, note.Total, 1180.0, "debit note total may exceed the invoice total")
}

// TestCreditNote_RejectsOverInvoiceTotal proves the cap: prior credits plus a
// new credit can never exceed the invoice total; the exact boundary is allowed.
func TestCreditNote_RejectsOverInvoiceTotal(t *testing.T) {
	dbConn, svcs := openMigratedDB(t)
	ctx := shared.ContextWithTenantID(context.Background(), shared.TenantID("tenant-cap"))
	insertNoteTestInvoice(t, dbConn, "tenant-cap", "inv-cap")

	_, err := svcs.Notes.CreateCreditNote(ctx, service.NoteRequest{
		InvoiceID: "inv-cap", Reason: "partial discount", TaxableValue: 1000,
	})
	require.NoError(t, err, "first credit of 1000 fits within invoice total 1180")

	_, err = svcs.Notes.CreateCreditNote(ctx, service.NoteRequest{
		InvoiceID: "inv-cap", Reason: "second discount", TaxableValue: 500,
	})
	require.ErrorIs(t, err, service.ErrNoteExceedsInvoiceTotal, "500 more would exceed 1180")

	var n int
	require.NoError(t, dbConn.QueryRow(
		`SELECT COUNT(*) FROM credit_debit_notes WHERE invoice_id='inv-cap'`).Scan(&n))
	assert.Equal(t, 1, n, "rejected note must not be persisted")

	// Boundary: remaining headroom is exactly 180.
	_, err = svcs.Notes.CreateCreditNote(ctx, service.NoteRequest{
		InvoiceID: "inv-cap", Reason: "final rate correction", TaxableValue: 180,
	})
	require.NoError(t, err, "a credit landing exactly on the invoice total must pass")
}

// TestCreditNote_TenantIsolation proves cross-tenant access resolves to
// not-found and listings never leak other tenants' notes.
func TestCreditNote_TenantIsolation(t *testing.T) {
	dbConn, svcs := openMigratedDB(t)
	ctxA := shared.ContextWithTenantID(context.Background(), shared.TenantID("tenant-A"))
	ctxB := shared.ContextWithTenantID(context.Background(), shared.TenantID("tenant-B"))
	insertNoteTestInvoice(t, dbConn, "tenant-A", "inv-iso-a")

	_, err := svcs.Notes.CreateCreditNote(ctxA, service.NoteRequest{
		InvoiceID: "inv-iso-a", Reason: "legit correction", TaxableValue: 10,
	})
	require.NoError(t, err)

	// Tenant B cannot annotate tenant A's invoice — same answer as a bogus id.
	_, err = svcs.Notes.CreateCreditNote(ctxB, service.NoteRequest{
		InvoiceID: "inv-iso-a", Reason: "hostile correction", TaxableValue: 10,
	})
	require.ErrorIs(t, err, domain.ErrInvoiceNotFound)

	notesB, err := svcs.Notes.GetNotesForInvoice(ctxB, "inv-iso-a")
	require.NoError(t, err)
	assert.Empty(t, notesB, "tenant B listing must not leak tenant A notes")

	notesA, err := svcs.Notes.GetNotesForInvoice(ctxA, "inv-iso-a")
	require.NoError(t, err)
	assert.Len(t, notesA, 1, "owner still sees its own note")

	var leaked int64
	require.NoError(t, dbConn.QueryRow(
		`SELECT COUNT(*) FROM credit_debit_notes WHERE tenant_id='tenant-B'`).Scan(&leaked))
	assert.Zero(t, leaked, "no rows may be written under tenant B")
}

// TestCreditNote_ValidationRules covers reason-required and positive-total
// guards (both fail before any DB write).
func TestCreditNote_ValidationRules(t *testing.T) {
	dbConn, svcs := openMigratedDB(t)
	ctx := shared.ContextWithTenantID(context.Background(), shared.TenantID("tenant-val"))
	insertNoteTestInvoice(t, dbConn, "tenant-val", "inv-val")

	cases := []struct {
		name    string
		req     service.NoteRequest
		wantErr error
	}{
		{
			name:    "missing_reason",
			req:     service.NoteRequest{InvoiceID: "inv-val", TaxableValue: 10},
			wantErr: service.ErrNoteReasonRequired,
		},
		{
			name:    "zero_total",
			req:     service.NoteRequest{InvoiceID: "inv-val", Reason: "why", TaxableValue: 0},
			wantErr: service.ErrNoteInvalidAmount,
		},
		{
			name:    "negative_taxable",
			req:     service.NoteRequest{InvoiceID: "inv-val", Reason: "why", TaxableValue: -5},
			wantErr: service.ErrNoteInvalidAmount,
		},
		{
			name:    "bogus_invoice",
			req:     service.NoteRequest{InvoiceID: "inv-missing", Reason: "why", TaxableValue: 10},
			wantErr: domain.ErrInvoiceNotFound,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svcs.Notes.CreateCreditNote(ctx, tc.req)
			require.ErrorIs(t, err, tc.wantErr)
			_, err = svcs.Notes.CreateDebitNote(ctx, tc.req)
			require.ErrorIs(t, err, tc.wantErr)
		})
	}

	var n int
	require.NoError(t, dbConn.QueryRow(
		`SELECT COUNT(*) FROM credit_debit_notes`).Scan(&n))
	assert.Equal(t, 0, n, "rejected requests must leave no rows behind")
}

// TestCreditNote_LedgerEntryWritten proves the money-ledger hook fires after
// creation: credit note → adjustment DEBIT, debit note → adjustment CREDIT,
// ref_table='credit_notes', ref_id=note id.
func TestCreditNote_LedgerEntryWritten(t *testing.T) {
	dbConn, svcs := openMigratedDB(t)
	ctx := shared.ContextWithTenantID(context.Background(), shared.TenantID("tenant-ledger"))
	insertNoteTestInvoice(t, dbConn, "tenant-ledger", "inv-ledger-cn")

	cn, err := svcs.Notes.CreateCreditNote(ctx, service.NoteRequest{
		InvoiceID: "inv-ledger-cn", Reason: "rate correction", TaxableValue: 118.05,
	})
	require.NoError(t, err)

	dn, err := svcs.Notes.CreateDebitNote(ctx, service.NoteRequest{
		InvoiceID: "inv-ledger-cn", Reason: "extra stop", TaxableValue: 50,
	})
	require.NoError(t, err)

	assertLedger := func(noteID, direction string, amountMinor int64) {
		t.Helper()
		var gotDir string
		var gotAmount int64
		require.NoError(t, dbConn.QueryRow(
			`SELECT direction, amount_minor FROM money_ledger
			 WHERE txn_type='adjustment' AND ref_table='credit_notes' AND ref_id=?`,
			noteID).Scan(&gotDir, &gotAmount))
		assert.Equal(t, direction, gotDir)
		assert.Equal(t, amountMinor, gotAmount)
	}
	assertLedger(cn.ID, "debit", 11805)
	assertLedger(dn.ID, "credit", 5000)
}

// TestMigration00098_RoundTrip proves the migration applies and rolls back
// cleanly in in-memory SQLite.
func TestMigration00098_RoundTrip(t *testing.T) {
	db, err := sql.Open("sqlite",
		fmt.Sprintf("file:rt98_%d?mode=memory&cache=shared&_pragma=journal_mode(WAL)", time.Now().UnixNano()))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, "../../db/migrations"))
	_, _ = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES
			('1','Default','default'), ('2','Tenant 2','tenant-2'), ('7','Tenant 7','tenant-7'), ('9','Tenant 9','tenant-9'),
			('other-tenant','Other Tenant','other-tenant'), ('another-tenant','Another Tenant','another-tenant'),
			('tenant-1','Test Tenant 1','tenant-1'), ('tenant-2','Test Tenant 2','tenant-2b'),
			('tenant-7','Test Tenant 7','tenant-7b'), ('tenant-9','Test Tenant 9','tenant-9b'),
			('tenant-999','Test Tenant 999','tenant-999'), ('tenant-a','Tenant A','tenant-a'),
			('tenant-b','Tenant B','tenant-b'), ('tenant-A','Tenant A Cap','tenant-a-cap'),
			('tenant-B','Tenant B Cap','tenant-b2'), ('tenant-zz','Tenant ZZ','tenant-zz'),
			('tenant-seq','Tenant Seq','tenant-seq'), ('tenant-cap','Tenant Cap','tenant-cap'),
			('tenant-dn','Tenant DN','tenant-dn'), ('tenant-ledger','Tenant Ledger','tenant-ledger'),
			('tenant-val','Tenant Val','tenant-val'), ('tenant-fmt','Test Tenant FMT','tenant-fmt'),
			('tenant-loop','Test Tenant Loop','tenant-loop'), ('tn-b','Tenant TN-B','tn-b'),
			('tn-kpi','Tenant TN-KPI','tn-kpi'), ('tenant-c','Tenant C','tenant-c'),
			('tenant-d','Tenant D','tenant-d'), ('tenant-forged','Tenant Forged','tenant-forged'),
			('tenant-42','Tenant 42','tenant-42'), ('test-tenant','Test Tenant','test-tenant'),
			('acme','Acme','acme'), ('beta','Beta','beta')`)
	_, _ = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES
			('1','Default','default'), ('2','Tenant 2','tenant-2'), ('7','Tenant 7','tenant-7'), ('9','Tenant 9','tenant-9'),
			('other-tenant','Other Tenant','other-tenant'), ('another-tenant','Another Tenant','another-tenant'),
			('tenant-1','Test Tenant 1','tenant-1'), ('tenant-2','Test Tenant 2','tenant-2b'),
			('tenant-7','Test Tenant 7','tenant-7b'), ('tenant-9','Test Tenant 9','tenant-9b'),
			('tenant-999','Test Tenant 999','tenant-999'), ('tenant-a','Tenant A','tenant-a'),
			('tenant-b','Tenant B','tenant-b'), ('tenant-A','Tenant A Cap','tenant-a-cap'),
			('tenant-B','Tenant B Cap','tenant-b2'), ('tenant-zz','Tenant ZZ','tenant-zz'),
			('tenant-seq','Tenant Seq','tenant-seq'), ('tenant-cap','Tenant Cap','tenant-cap'),
			('tenant-dn','Tenant DN','tenant-dn'), ('tenant-ledger','Tenant Ledger','tenant-ledger'),
			('tenant-val','Tenant Val','tenant-val'), ('tenant-fmt','Test Tenant FMT','tenant-fmt'),
			('tenant-loop','Test Tenant Loop','tenant-loop'), ('tn-b','Tenant TN-B','tn-b'),
			('tn-kpi','Tenant TN-KPI','tn-kpi'), ('tenant-c','Tenant C','tenant-c'),
			('tenant-d','Tenant D','tenant-d'), ('tenant-forged','Tenant Forged','tenant-forged'),
			('tenant-42','Tenant 42','tenant-42'), ('test-tenant','Test Tenant','test-tenant'),
			('acme','Acme','acme'), ('beta','Beta','beta')`)

	tableExists := func(name string) bool {
		var n int
		_ = db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&n)
		return n == 1
	}
	require.True(t, tableExists("credit_debit_notes"), "credit_debit_notes must exist after up")
	require.True(t, tableExists("note_sequences"), "note_sequences must exist after up")

	// CHECK constraint on note_type is enforced at the DDL level.
	_, err = db.Exec(`INSERT INTO credit_debit_notes (id, tenant_id, note_number, note_type, invoice_id, reason, taxable_value, total)
		VALUES ('m1','t1','CN/X/0001','refund','inv-x','why',1,1)`)
	assert.Error(t, err, "CHECK constraint must reject invalid note_type")

	require.NoError(t, goose.DownTo(db, "../../db/migrations", 97))
	assert.False(t, tableExists("credit_debit_notes"), "credit_debit_notes must drop after down to 97")
	assert.False(t, tableExists("note_sequences"), "note_sequences must drop after down to 97")

	require.NoError(t, goose.Up(db, "../../db/migrations"))
	_, _ = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES
			('1','Default','default'), ('2','Tenant 2','tenant-2'), ('7','Tenant 7','tenant-7'), ('9','Tenant 9','tenant-9'),
			('other-tenant','Other Tenant','other-tenant'), ('another-tenant','Another Tenant','another-tenant'),
			('tenant-1','Test Tenant 1','tenant-1'), ('tenant-2','Test Tenant 2','tenant-2b'),
			('tenant-7','Test Tenant 7','tenant-7b'), ('tenant-9','Test Tenant 9','tenant-9b'),
			('tenant-999','Test Tenant 999','tenant-999'), ('tenant-a','Tenant A','tenant-a'),
			('tenant-b','Tenant B','tenant-b'), ('tenant-A','Tenant A Cap','tenant-a-cap'),
			('tenant-B','Tenant B Cap','tenant-b2'), ('tenant-zz','Tenant ZZ','tenant-zz'),
			('tenant-seq','Tenant Seq','tenant-seq'), ('tenant-cap','Tenant Cap','tenant-cap'),
			('tenant-dn','Tenant DN','tenant-dn'), ('tenant-ledger','Tenant Ledger','tenant-ledger'),
			('tenant-val','Tenant Val','tenant-val'), ('tenant-fmt','Test Tenant FMT','tenant-fmt'),
			('tenant-loop','Test Tenant Loop','tenant-loop'), ('tn-b','Tenant TN-B','tn-b'),
			('tn-kpi','Tenant TN-KPI','tn-kpi'), ('tenant-c','Tenant C','tenant-c'),
			('tenant-d','Tenant D','tenant-d'), ('tenant-forged','Tenant Forged','tenant-forged'),
			('tenant-42','Tenant 42','tenant-42'), ('test-tenant','Test Tenant','test-tenant'),
			('acme','Acme','acme'), ('beta','Beta','beta')`)
	assert.True(t, tableExists("credit_debit_notes"), "re-up restores credit_debit_notes")
	assert.True(t, tableExists("note_sequences"), "re-up restores note_sequences")
}
