package db

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// TestMigration00190RazorpayOrderBinding proves the order->invoice binding
// table applies and rolls back cleanly.
func TestMigration00190RazorpayOrderBinding(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "mig.db")
	database, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer database.Close()

	ctx := context.Background()
	migFS, err := fs.Sub(Migrations, "migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectSQLite3, database, migFS)
	require.NoError(t, err)

	_, err = provider.Up(ctx)
	require.NoError(t, err)

	_, err = database.Exec(
		`INSERT INTO razorpay_orders (order_id, tenant_id, invoice_id, amount_paise)
		 VALUES ('order_1', 'tenant-1', 'inv-1', 10000)`,
	)
	require.NoError(t, err, "binding insert must pass after up")

	var got int64
	require.NoError(t, database.QueryRow(
		`SELECT amount_paise FROM razorpay_orders WHERE order_id = 'order_1'`,
	).Scan(&got))
	require.Equal(t, int64(10000), got)

	_, err = provider.DownTo(ctx, 179)
	require.NoError(t, err)

	_, err = database.Exec(
		`INSERT INTO razorpay_orders (order_id, tenant_id, invoice_id, amount_paise)
		 VALUES ('order_2', 'tenant-1', 'inv-1', 10000)`,
	)
	require.Error(t, err, "table must be gone after down to 00179")

	_, err = provider.Up(ctx)
	require.NoError(t, err)
	assertForeignKeyCheckClean(t, database, "00190 re-up")
}
