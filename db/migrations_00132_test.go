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

// TestMigration00132VehicleExpiryNullableUpAndDown proves 00132 applies AND
// rolls back: doc-date columns go nullable, real expiries survive the rebuild
// byte-for-byte, fresh NULL-expiry inserts succeed, and DownTo(131) restores
// NOT NULL (re-up restores nullability). No backfill by design: a fabricated
// +1y date is indistinguishable from a real one, so legacy rows keep values.
func TestMigration00132VehicleExpiryNullableUpAndDown(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "mig.db")
	database, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer database.Close()

	ctx := context.Background()
	migFS, err := fs.Sub(Migrations, "migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectSQLite3, database, migFS)
	require.NoError(t, err)

	_, err = provider.UpTo(ctx, 131)
	require.NoError(t, err)

	_, err = database.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('t132', 'T132 Fleet', 't132-fleet')`)
	require.NoError(t, err)
	_, err = database.Exec(`INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, fuel_type, insurance_expiry, fitness_expiry, permit_expiry, status, tenant_id)
		VALUES ('veh-real', 'MH12AB1234', 'MH12AB1234', 'truck', 5000, 'diesel', '2027-06-01', '2027-06-01', '2027-06-01', 'available', 't132')`)
	require.NoError(t, err)

	_, err = provider.Up(ctx)
	require.NoError(t, err)

	var ins, fit, per sql.NullString
	require.NoError(t, database.QueryRow(
		`SELECT date(insurance_expiry), date(fitness_expiry), date(permit_expiry) FROM vehicles WHERE id = 'veh-real'`,
	).Scan(&ins, &fit, &per))
	require.Equal(t, "2027-06-01", ins.String, "real expiries must survive the rebuild")
	require.Equal(t, "2027-06-01", fit.String)
	require.Equal(t, "2027-06-01", per.String)

	// Fresh side-effect registration without doc dates must insert cleanly.
	_, err = database.Exec(`INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, fuel_type, status, tenant_id)
		VALUES ('veh-new', 'DL1LN9999', 'DL1LN9999', 'truck', 5000, 'diesel', 'available', 't132')`)
	require.NoError(t, err, "NULL doc dates must insert after 00132")
	require.NoError(t, database.QueryRow(
		`SELECT insurance_expiry FROM vehicles WHERE id = 'veh-new'`,
	).Scan(&ins))
	require.False(t, ins.Valid)

	assertForeignKeyCheckClean(t, database, "00132 up")

	_, err = provider.DownTo(ctx, 131)
	require.NoError(t, err)

	var notnull int
	require.NoError(t, database.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('vehicles') WHERE name IN ('insurance_expiry','fitness_expiry','permit_expiry') AND "notnull" = 1`,
	).Scan(&notnull))
	require.Equal(t, 3, notnull, "all three columns must be NOT NULL again after down to 131")

	_, err = provider.Up(ctx)
	require.NoError(t, err)
	assertForeignKeyCheckClean(t, database, "00132 re-up")
}
