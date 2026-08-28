package service_test

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/config"
	"transport-app/internal/domain"
	"transport-app/internal/events"
	"transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

// openMigratedDB opens a fresh in-memory SQLite and applies every migration,
// mirroring setupComplianceTestDB. Returns the raw DB plus services wired to it.
func openMigratedDB(t *testing.T) (*sql.DB, *service.Services) {
	t.Helper()
	name := fmt.Sprintf("test_ledger_%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	dbConn, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	t.Cleanup(func() { _ = dbConn.Close() })
	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.Up(dbConn, "../../db/migrations"))
	// 00104/00105 strict FK — seed all common test tenants (00103 seeds deleted in 00105 prod cleanup).
	_, _ = dbConn.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES
		('1','Default','default'), ('2','Tenant 2','tenant-2'), ('tenant-1','Test Tenant 1','tenant-1'),
		('tenant-val','Tenant Val','tenant-val'), ('tenant-seq','Tenant Seq','tenant-seq'),
		('tenant-a','Tenant A','tenant-a'), ('tenant-b','Tenant B','tenant-b'),
		('acme','Acme','acme'), ('beta','Beta','beta'), ('tenant-ledger','Tenant Ledger','tenant-ledger')`)

	repo := sqlite.NewRepository(dbConn)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svcs := service.NewServices(repo, &config.Config{}, log, events.NewInMemoryBus())
	return dbConn, svcs
}

func newTestLedger(t *testing.T) (*sql.DB, *service.MoneyLedgerService) {
	t.Helper()
	dbConn, _ := openMigratedDB(t)
	repo := sqlite.NewRepository(dbConn)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return dbConn, service.NewMoneyLedgerService(repo, log)
}

func TestMoneyLedger_AppendEntry_HappyPath(t *testing.T) {
	dbConn, ledger := newTestLedger(t)
	ctx := shared.ContextWithTenantID(context.Background(), shared.TenantID("tenant-1"))

	err := ledger.AppendEntry(ctx, service.LedgerEntry{
		TxnType:     "payment_recorded",
		RefTable:    "payments",
		RefID:       "pay-1",
		Direction:   "credit",
		AmountMinor: 12345,
	})
	require.NoError(t, err)

	var (
		tenantID, txnType, refTable, refID, direction, currency string
		amountMinor                                             int64
	)
	require.NoError(t, dbConn.QueryRow(
		`SELECT tenant_id, txn_type, ref_table, ref_id, direction, amount_minor, currency
		 FROM money_ledger WHERE ref_table='payments' AND ref_id='pay-1'`,
	).Scan(&tenantID, &txnType, &refTable, &refID, &direction, &amountMinor, &currency))
	assert.Equal(t, "tenant-1", tenantID)
	assert.Equal(t, "payment_recorded", txnType)
	assert.Equal(t, "payments", refTable)
	assert.Equal(t, "pay-1", refID)
	assert.Equal(t, "credit", direction)
	assert.Equal(t, int64(12345), amountMinor)
	assert.Equal(t, "INR", currency, "currency must default to INR")

	var n int
	require.NoError(t, dbConn.QueryRow(`SELECT COUNT(*) FROM money_ledger`).Scan(&n))
	assert.Equal(t, 1, n, "append-only table holds exactly the one row")
}

func TestMoneyLedger_AppendEntry_RejectsWhitelistViolations(t *testing.T) {
	_, ledger := newTestLedger(t)
	ctx := shared.ContextWithTenantID(context.Background(), shared.TenantID("tenant-1"))

	cases := []struct {
		name   string
		mutate func(*service.LedgerEntry)
	}{
		{"bad_txn_type", func(e *service.LedgerEntry) { e.TxnType = "hacked_type" }},
		{"bad_direction", func(e *service.LedgerEntry) { e.Direction = "sideways" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := service.LedgerEntry{
				TxnType:     "adjustment",
				RefTable:    "invoices",
				RefID:       "inv-1",
				Direction:   "debit",
				AmountMinor: 100,
			}
			tc.mutate(&e)
			err := ledger.AppendEntry(ctx, e)
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "insert", "must be rejected before touching the database")
		})
	}
}

func TestMoneyLedger_AppendEntry_RejectsNegativeAmount(t *testing.T) {
	_, ledger := newTestLedger(t)
	ctx := shared.ContextWithTenantID(context.Background(), shared.TenantID("tenant-1"))

	err := ledger.AppendEntry(ctx, service.LedgerEntry{
		TxnType:     "kharcha_approved",
		RefTable:    "driver_expenses",
		RefID:       "de-1",
		Direction:   "debit",
		AmountMinor: -500,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "negative amount_minor")
}

func TestMoneyLedger_AppendEntry_FailsClosedWithoutTenant(t *testing.T) {
	_, ledger := newTestLedger(t)

	err := ledger.AppendEntry(context.Background(), service.LedgerEntry{
		TxnType:     "payment_recorded",
		RefTable:    "payments",
		RefID:       "pay-x",
		Direction:   "credit",
		AmountMinor: 1,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tenant not set")
}

func TestMigration00097_MoneyLedger_DDLAndConstraints(t *testing.T) {
	dbConn, _ := newTestLedger(t)

	// CHECK constraints are enforced at the DDL level too.
	bad := []string{
		`INSERT INTO money_ledger (id, tenant_id, txn_type, ref_table, ref_id, direction, amount_minor)
		 VALUES ('m1', 't1', 'bogus', 'p', 'p1', 'debit', 1)`,
		`INSERT INTO money_ledger (id, tenant_id, txn_type, ref_table, ref_id, direction, amount_minor)
		 VALUES ('m2', 't1', 'payment_recorded', 'p', 'p2', 'up', 1)`,
		`INSERT INTO money_ledger (id, tenant_id, txn_type, ref_table, ref_id, direction, amount_minor)
		 VALUES ('m3', 't1', 'payment_recorded', 'p', 'p3', 'credit', -1)`,
	}
	for _, q := range bad {
		_, err := dbConn.Exec(q)
		assert.Error(t, err, "CHECK constraint must reject: %s", q)
	}

	var idx int
	require.NoError(t, dbConn.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name IN
		 ('idx_money_ledger_tenant_created','idx_money_ledger_ref')`).Scan(&idx))
	assert.Equal(t, 2, idx, "both ledger indexes must exist after up")
}

func TestMigration00097_MoneyLedger_RoundTrip(t *testing.T) {
	name := fmt.Sprintf("rt97_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	defer db.Close()

	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, "../../db/migrations"))

	tableExists := func() bool {
		var n int
		db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='money_ledger'`).Scan(&n)
		return n == 1
	}
	require.True(t, tableExists(), "money_ledger must exist after up")

	require.NoError(t, goose.DownTo(db, "../../db/migrations", 96))
	assert.False(t, tableExists(), "money_ledger must be dropped after down to 96")

	require.NoError(t, goose.Up(db, "../../db/migrations"))
	assert.True(t, tableExists(), "re-up restores money_ledger")
}

func TestToMinor_RoundHalfUp(t *testing.T) {
	cases := []struct {
		in   float64
		want int64
	}{
		{0, 0},
		{1, 100},
		{10.55, 1055},
		{0.004, 0},    // below half-paise rounds down
		{0.005, 1},    // half-paise rounds UP
		{0.006, 1},    // above half rounds up
		{2.3449, 234}, // remainder < half
		{2.345, 235},  // binary-float trap: 2.345*100 != 235 in float64
		{99.999, 10000},
		{125.50, 12550},
		{-2.345, -235}, // mirrored half-away-from-zero
	}
	for _, tc := range cases {
		assert.Equalf(t, tc.want, service.ToMinor(tc.in), "ToMinor(%v)", tc.in)
	}
}

func TestPaymentService_RecordPayment_WritesLedgerCredit(t *testing.T) {
	dbConn, svcs := openMigratedDB(t)
	ctx := shared.ContextWithTenantID(context.Background(), shared.TenantID("tenant-1"))

	_, err := dbConn.Exec(
		`INSERT INTO customers (id, name, phone) VALUES ('cus-1', 'Test Customer', '+919999999999')`)
	require.NoError(t, err)
	_, err = dbConn.Exec(
		`INSERT INTO invoices (id, invoice_number, booking_id, customer_id, subtotal, total, tenant_id)
		 VALUES ('inv-ledger-1', 'INV-LG-1', 'bk-1', 'cus-1', 500.0, 500.0, 'tenant-1')`)
	require.NoError(t, err)

	paid, err := svcs.Payments.RecordPayment(ctx, domain.InvoiceID("inv-ledger-1"),
		125.50, domain.PaymentMethodCash, "ref-1", "test remarks", "")
	require.NoError(t, err)

	var (
		txnType, refTable, refID, direction string
		amountMinor                         int64
	)
	require.NoError(t, dbConn.QueryRow(
		`SELECT txn_type, ref_table, ref_id, direction, amount_minor
		 FROM money_ledger WHERE txn_type='payment_recorded'`,
	).Scan(&txnType, &refTable, &refID, &direction, &amountMinor))
	assert.Equal(t, "payments", refTable)
	assert.Equal(t, string(paid.ID), refID, "ledger must reference the created payment id")
	assert.Equal(t, "credit", direction)
	assert.Equal(t, int64(12550), amountMinor)
}
