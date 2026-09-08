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

// TestMigration00130TaxVerifyStatusUpAndDown proves 00130 applies AND rolls
// back: new profiles default to UNVERIFIED, bogus states are rejected, and
// DownTo(129) removes both columns (re-up restores them).
func TestMigration00130TaxVerifyStatusUpAndDown(t *testing.T) {
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

	_, err = database.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tv1', 'TV Fleet', 'tv-fleet')`)
	require.NoError(t, err)
	_, err = database.Exec(`INSERT INTO tenant_company_profiles (tenant_id, company_name) VALUES ('tv1', 'TV Fleet')`)
	require.NoError(t, err)

	var status string
	var verifiedAt sql.NullString
	require.NoError(t, database.QueryRow(
		`SELECT gstin_verify_status, gstin_verified_at FROM tenant_company_profiles WHERE tenant_id = 'tv1'`,
	).Scan(&status, &verifiedAt))
	require.Equal(t, "UNVERIFIED", status)
	require.False(t, verifiedAt.Valid, "verified_at stays NULL until a live lookup stamps it")

	_, err = database.Exec(`UPDATE tenant_company_profiles SET gstin_verify_status = 'BOGUS' WHERE tenant_id = 'tv1'`)
	require.Error(t, err, "bogus verify state must violate the CHECK")

	_, err = database.Exec(`UPDATE tenant_company_profiles SET gstin_verify_status = 'VERIFIED', gstin_verified_at = CURRENT_TIMESTAMP WHERE tenant_id = 'tv1'`)
	require.NoError(t, err, "VERIFIED + stamp is the worker's happy path")

	_, err = provider.DownTo(ctx, 129)
	require.NoError(t, err)

	var n int
	require.NoError(t, database.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('tenant_company_profiles') WHERE name IN ('gstin_verify_status', 'gstin_verified_at')`,
	).Scan(&n))
	require.Equal(t, 0, n, "both columns must be gone after down to 129")

	_, err = provider.Up(ctx)
	require.NoError(t, err)
	assertForeignKeyCheckClean(t, database, "00130 re-up")
}
