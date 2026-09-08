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

// TestMigration00133UserEmailVerifiedUpAndDown proves 00133 applies AND rolls
// back: the flag column exists and defaults to NULL (unverified), stamping a
// verification time sticks, and DownTo(132) drops the column (re-up restores).
func TestMigration00133UserEmailVerifiedUpAndDown(t *testing.T) {
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

	_, err = database.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('t133', 'T133 Fleet', 't133-fleet')`)
	require.NoError(t, err)
	_, err = database.Exec(`INSERT INTO users (id, email, password_hash, name, role_id, status) VALUES ('u133', 'verify133@test.com', 'x', 'Verify User', 1, 'active')`)
	require.NoError(t, err)

	var verifiedAt sql.NullString
	require.NoError(t, database.QueryRow(
		`SELECT email_verified_at FROM users WHERE id = 'u133'`,
	).Scan(&verifiedAt))
	require.False(t, verifiedAt.Valid, "fresh users are unverified (NULL)")

	_, err = database.Exec(`UPDATE users SET email_verified_at = CURRENT_TIMESTAMP WHERE id = 'u133'`)
	require.NoError(t, err)
	require.NoError(t, database.QueryRow(
		`SELECT email_verified_at FROM users WHERE id = 'u133'`,
	).Scan(&verifiedAt))
	require.True(t, verifiedAt.Valid, "consumed link stamps the flag")

	_, err = provider.DownTo(ctx, 132)
	require.NoError(t, err)

	var n int
	require.NoError(t, database.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('users') WHERE name = 'email_verified_at'`,
	).Scan(&n))
	require.Equal(t, 0, n, "column must be gone after down to 132")

	_, err = provider.Up(ctx)
	require.NoError(t, err)
	assertForeignKeyCheckClean(t, database, "00133 re-up")
}
