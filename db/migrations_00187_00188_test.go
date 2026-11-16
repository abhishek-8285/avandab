package db_test

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// TestMigrations00181_00182_AuthTables proves the persistent one-time token
// table (M1) and login throttle table (M3) apply and roll back, and that both
// are usable with the columns the Go stores query.
func TestMigrations00187_00188_AuthTables(t *testing.T) {
	name := fmt.Sprintf("test_mig_00181_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.UpTo(db, "migrations", 188))

	tableExists := func(table string) bool {
		var n int
		require.NoError(t, db.QueryRow(
			`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&n))
		return n == 1
	}
	require.True(t, tableExists("auth_one_time_tokens"), "00181 must create auth_one_time_tokens")
	require.True(t, tableExists("login_throttle"), "00182 must create login_throttle")

	// Prove the schemas are writable with the store's columns.
	_, err = db.Exec(`INSERT INTO auth_one_time_tokens (token_hash, email, purpose, expires_at)
		VALUES ('hash-1', 'user@example.com', 'password_reset', ?)`, time.Now().Add(time.Hour))
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO login_throttle (email_key, failures, locked_until, updated_at)
		VALUES ('key-1', 5, ?, ?)`, time.Now().Add(15*time.Minute), time.Now())
	require.NoError(t, err)

	require.NoError(t, goose.DownTo(db, "migrations", 186))
	require.False(t, tableExists("auth_one_time_tokens"), "down must drop auth_one_time_tokens")
	require.False(t, tableExists("login_throttle"), "down must drop login_throttle")
}
