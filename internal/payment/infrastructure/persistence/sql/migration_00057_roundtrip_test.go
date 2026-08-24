package sql

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestMigration00057_RazorpayFields_RoundTrip(t *testing.T) {
	name := fmt.Sprintf("rt57_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	defer db.Close()

	_ = goose.SetDialect("sqlite")

	require.NoError(t, goose.Up(db, "../../../../../db/migrations"), "goose up failed")

	colExists := func(col string) bool {
		var n int
		db.QueryRow(`SELECT count(*) FROM pragma_table_info('payments') WHERE name=?`, col).Scan(&n)
		return n == 1
	}
	idxExists := func(idx string) bool {
		var n int
		db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='index' AND name=?`, idx).Scan(&n)
		return n == 1
	}

	for _, col := range []string{"razorpay_order_id", "razorpay_payment_id", "razorpay_signature", "webhook_event_id"} {
		assert.Truef(t, colExists(col), "payments.%s must exist after up", col)
	}
	assert.True(t, idxExists("idx_payments_webhook_event"), "idx_payments_webhook_event must exist after up")
	assert.True(t, idxExists("idx_payments_razorpay_payment"), "idx_payments_razorpay_payment must exist after up")

	// Unique partial index blocks a replayed razorpay payment id per tenant,
	// while rows without razorpay ids (cash/upi) stay unconstrained.
	_, err = db.Exec(`INSERT INTO payments (id, tenant_id, invoice_id, payment_date, amount, method, razorpay_payment_id)
		VALUES ('p1', '1', 'inv1', datetime('now'), 100, 'razorpay', 'pay_dup')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO payments (id, tenant_id, invoice_id, payment_date, amount, method, razorpay_payment_id)
		VALUES ('p2', '1', 'inv1', datetime('now'), 100, 'razorpay', 'pay_dup')`)
	assert.Error(t, err, "duplicate razorpay_payment_id within tenant must violate unique index")
	_, err = db.Exec(`INSERT INTO payments (id, tenant_id, invoice_id, payment_date, amount, method, razorpay_payment_id)
		VALUES ('p3', '2', 'inv2', datetime('now'), 100, 'razorpay', 'pay_dup')`)
	assert.NoError(t, err, "same razorpay_payment_id in ANOTHER tenant must be allowed")
	_, err = db.Exec(`INSERT INTO payments (id, tenant_id, invoice_id, payment_date, amount, method)
		VALUES ('p4', '1', 'inv1', datetime('now'), 50, 'cash')`)
	assert.NoError(t, err, "payment without razorpay fields must not be constrained")

	require.NoError(t, goose.DownTo(db, "../../../../../db/migrations", 56), "goose down to 56 failed")
	for _, col := range []string{"razorpay_order_id", "razorpay_payment_id", "razorpay_signature", "webhook_event_id"} {
		assert.Falsef(t, colExists(col), "payments.%s must be dropped after down", col)
	}
	assert.False(t, idxExists("idx_payments_webhook_event"), "idx_payments_webhook_event must be dropped after down")
	assert.False(t, idxExists("idx_payments_razorpay_payment"), "idx_payments_razorpay_payment must be dropped after down")

	require.NoError(t, goose.Up(db, "../../../../../db/migrations"), "goose up again failed")
	assert.True(t, colExists("webhook_event_id"), "re-up must restore webhook_event_id")
}
