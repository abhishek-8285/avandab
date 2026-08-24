package test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/service"
)

// seedMoneyStripFixtures inserts invoice/expense/toll/payment rows for two
// tenants on `day`. Totals:
//
//	tenant-a: revenue 4200 (two paid invoices), spent 550 (fuel 300 +
//	toll 250), receivables 700 (outstanding invoice 1000 minus one 300
//	payment).
//	tenant-b: revenue 999, everything else zero.
//
// NOTE: maintenance_records carries no tenant_id column (00044), so the
// P&L maintenance query cannot attribute rows per-tenant; fixtures omit it.
func seedMoneyStripFixtures(t *testing.T, db *sql.DB, day time.Time) {
	t.Helper()
	dayStr := day.Format("2006-01-02")
	ctx := context.Background()

	must := func(query string, args ...any) {
		_, err := db.ExecContext(ctx, query, args...)
		require.NoError(t, err, "seed: %s", query)
	}

	// Revenue — paid invoices dated today (payment_status='paid'), each
	// settled through the payments ledger like real RecordPayment flows.
	must(`INSERT INTO invoices (id, invoice_number, booking_id, customer_id, subtotal, tax, total, payment_status, status, paid_amount, tenant_id, created_at, updated_at)
	      VALUES ('inv-a1','INVA1','bk-x','cust-x',2000,0,2000,'paid','paid',2000,'tenant-a',?,?)`, dayStr+" 10:00:00", dayStr)
	must(`INSERT INTO invoices (id, invoice_number, booking_id, customer_id, subtotal, tax, total, payment_status, status, paid_amount, tenant_id, created_at, updated_at)
	      VALUES ('inv-a2','INVA2','bk-x','cust-x',2200,0,2200,'paid','paid',2200,'tenant-a',?,?)`, dayStr+" 11:00:00", dayStr)
	must(`INSERT INTO payments (id, invoice_id, amount, payment_date, method, tenant_id, created_at)
	      VALUES ('pay-a1','inv-a1',2000,?,'cash','tenant-a',?)`, dayStr+" 10:05:00", dayStr)
	must(`INSERT INTO payments (id, invoice_id, amount, payment_date, method, tenant_id, created_at)
	      VALUES ('pay-a2','inv-a2',2200,?,'upi','tenant-a',?)`, dayStr+" 11:05:00", dayStr)

	// Receivables — outstanding 1000 with one 300 payment.
	must(`INSERT INTO invoices (id, invoice_number, booking_id, customer_id, subtotal, tax, total, payment_status, status, paid_amount, tenant_id, created_at, updated_at)
	      VALUES ('inv-a3','INVA3','bk-y','cust-y',1000,0,1000,'pending','outstanding',0,'tenant-a',?,?)`, dayStr+" 12:00:00", dayStr)
	must(`INSERT INTO payments (id, invoice_id, amount, payment_date, method, tenant_id, created_at, updated_at)
	      VALUES ('pay-a3','inv-a3',300,?,?,'upi','tenant-a',?)`, dayStr+" 13:00:00", "cash", dayStr)

	// Spent: fuel expense (not absorbed into any settlement line) + toll.
	must(`INSERT INTO driver_expenses (id, trip_id, driver_id, expense_type, category, amount, description, tenant_id, created_at)
	      VALUES ('de-fuel','tr-x','drv-x','fuel','fuel',300,'diesel','tenant-a',?)`, dayStr+" 09:30:00")
	must(`INSERT INTO fastag_transactions (id, tenant_id, tag_id, vehicle_id, amount, txn_timestamp, plaza_name)
	      VALUES ('ft-1','tenant-a','tag-x','veh-x',250,?,'plaza-x')`, dayStr+" 08:00:00")

	// Cross-tenant noise.
	must(`INSERT INTO invoices (id, invoice_number, booking_id, customer_id, subtotal, tax, total, payment_status, status, paid_amount, tenant_id, created_at, updated_at)
	      VALUES ('inv-b1','INVB1','bk-z','cust-z',999,0,999,'paid','paid',999,'tenant-b',?,?)`, dayStr, dayStr)
	must(`INSERT INTO payments (id, invoice_id, amount, payment_date, method, tenant_id, created_at)
	      VALUES ('pay-b1','inv-b1',999,?,'cash','tenant-b',?)`, dayStr+" 12:05:00", dayStr)
}

// TestSpec22_MoneyStrip_MatchesReportTotals — Spec 22 §7 S2: strip sums
// equal the canonical P&L totals for the seeded fixtures, receivables use
// outstanding-balance semantics, and tenants are isolated.
func TestSpec22_MoneyStrip_MatchesReportTotals(t *testing.T) {
	db := NewTestDB(t)
	svc := service.NewPNLService(db)
	ctx := context.Background()
	day := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)

	seedMoneyStripFixtures(t, db, day)

	strip, err := svc.GetMoneyStrip(ctx, "tenant-a", day)
	require.NoError(t, err)
	assert.Equal(t, day.Format("2006-01-02"), strip.Date)
	assert.InDelta(t, 4200.0, strip.Revenue, 0.001, "revenue = sum of paid invoices")
	assert.InDelta(t, 550.0, strip.Spent, 0.001, "spent = fuel+toll")
	assert.InDelta(t, 700.0, strip.Receivables, 0.001, "receivables = outstanding minus payments")
	assert.Positive(t, strip.Spent)

	// Strip must equal the persisted P&L snapshot for the same day.
	snap, err := svc.GenerateDailySnapshot(ctx, "tenant-a", day)
	require.NoError(t, err)
	assert.InDelta(t, snap.Revenue, strip.Revenue, 0.001)
	assert.InDelta(t, snap.Expenses, strip.Spent, 0.001)

	other, err := svc.GetMoneyStrip(ctx, "tenant-b", day)
	require.NoError(t, err)
	assert.InDelta(t, 999.0, other.Revenue, 0.001)
	assert.InDelta(t, 0.0, other.Receivables, 0.001, "tenant-b has no open invoices")

	empty, err := svc.GetMoneyStrip(ctx, "tenant-zz", day)
	require.NoError(t, err)
	assert.InDelta(t, 0.0, empty.Revenue, 0.001)
}
