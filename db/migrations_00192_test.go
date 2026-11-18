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

// 00192 makes invoice numbers unique per tenant instead of globally.
func TestMigration00192InvoiceNumberPerTenant(t *testing.T) {
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

	_, err = database.Exec(`PRAGMA foreign_keys = OFF`)
	require.NoError(t, err)
	_, err = database.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES
		('tenant-a', 'A', 'tenant-a'), ('tenant-b', 'B', 'tenant-b')`)
	require.NoError(t, err)

	insert := func(id, tenant, number string) error {
		_, err := database.Exec(`INSERT INTO invoices
			(id, invoice_number, booking_id, customer_id, subtotal, total, tenant_id)
			VALUES (?, ?, 'bk-x', 'cust-x', 100, 100, ?)`, id, number, tenant)
		return err
	}
	require.NoError(t, insert("inv-a", "tenant-a", "INV/2026-27/0001"))
	require.NoError(t, insert("inv-b", "tenant-b", "INV/2026-27/0001"),
		"the same number must be allowed for a different tenant")
	require.Error(t, insert("inv-a2", "tenant-a", "INV/2026-27/0001"),
		"the same number must be rejected within one tenant")

	// Rollback needs globally unique numbers, so drop the cross-tenant duplicate.
	_, err = database.Exec(`DELETE FROM invoices WHERE id = 'inv-b'`)
	require.NoError(t, err)
	_, err = provider.DownTo(ctx, 181)
	require.NoError(t, err)
	require.NoError(t, insert("inv-a3", "tenant-a", "INV/2026-27/0002"))
	require.Error(t, insert("inv-b3", "tenant-b", "INV/2026-27/0002"),
		"after rollback the global UNIQUE must be back")

	_, err = database.Exec(`DELETE FROM invoices`)
	require.NoError(t, err)
	_, err = provider.Up(ctx)
	require.NoError(t, err)
	assertForeignKeyCheckClean(t, database, "00192 re-up")
}
