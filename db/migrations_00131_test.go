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

// TestMigration00131DriverLicenseNullableUpAndDown proves 00131 applies AND
// rolls back: license columns go nullable, legacy 'DL-PENDING' rows are
// backfilled to NULL/NULL, real licenses survive the rebuild byte-for-byte,
// and DownTo(130) restores NOT NULL (re-up restores nullability).
func TestMigration00131DriverLicenseNullableUpAndDown(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "mig.db")
	database, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer database.Close()

	ctx := context.Background()
	migFS, err := fs.Sub(Migrations, "migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectSQLite3, database, migFS)
	require.NoError(t, err)

	_, err = provider.UpTo(ctx, 130)
	require.NoError(t, err)

	_, err = database.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('t131', 'T131 Fleet', 't131-fleet')`)
	require.NoError(t, err)
	// Legacy placeholder row (what RegisterDriver wrote before this fix) plus
	// one real driver whose data must survive the rebuild untouched.
	_, err = database.Exec(`INSERT INTO drivers (id, driver_id, first_name, last_name, phone, license_number, license_expiry, status, tenant_id)
		VALUES ('drv-old', 'DRV-OLD', 'Old', 'Driver', '9000000001', 'DL-PENDING', date('now','+5 years'), 'available', 't131')`)
	require.NoError(t, err)
	_, err = database.Exec(`INSERT INTO drivers (id, driver_id, first_name, last_name, phone, email, license_number, license_expiry, status, tenant_id)
		VALUES ('drv-real', 'DRV-REAL', 'Real', 'Driver', '9000000002', 'real@example.com', 'MH1420210088991', '2032-12-31', 'available', 't131')`)
	require.NoError(t, err)

	_, err = provider.Up(ctx)
	require.NoError(t, err)

	var num, exp sql.NullString
	require.NoError(t, database.QueryRow(
		`SELECT license_number, license_expiry FROM drivers WHERE id = 'drv-old'`,
	).Scan(&num, &exp))
	require.False(t, num.Valid, "DL-PENDING number must backfill to NULL")
	require.False(t, exp.Valid, "fabricated expiry must backfill to NULL")

	require.NoError(t, database.QueryRow(
		`SELECT license_number, date(license_expiry) FROM drivers WHERE id = 'drv-real'`,
	).Scan(&num, &exp))
	require.Equal(t, "MH1420210088991", num.String, "real license must survive the rebuild")
	require.Equal(t, "2032-12-31", exp.String)

	// Fresh registration without a license must now insert cleanly.
	_, err = database.Exec(`INSERT INTO drivers (id, driver_id, first_name, last_name, phone, status, tenant_id)
		VALUES ('drv-new', 'DRV-NEW', 'New', 'Driver', '9000000003', 'available', 't131')`)
	require.NoError(t, err, "NULL license insert must succeed after 00131")

	assertForeignKeyCheckClean(t, database, "00131 up")

	_, err = provider.DownTo(ctx, 130)
	require.NoError(t, err)

	var notnull int
	require.NoError(t, database.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('drivers') WHERE name IN ('license_number','license_expiry') AND "notnull" = 1`,
	).Scan(&notnull))
	require.Equal(t, 2, notnull, "both columns must be NOT NULL again after down to 130")

	require.NoError(t, database.QueryRow(
		`SELECT license_number FROM drivers WHERE id = 'drv-old'`,
	).Scan(&num))
	require.Equal(t, "DL-PENDING", num.String, "down backfills NULLs to the legacy sentinel")

	_, err = provider.Up(ctx)
	require.NoError(t, err)
	assertForeignKeyCheckClean(t, database, "00131 re-up")
}
