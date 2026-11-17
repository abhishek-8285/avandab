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

// TestMigration00189_KharchaReadGrant proves the read surface is granted to
// every manager role and denied to drivers, whose trips:read previously opened
// the tenant-wide expense queue. Down must remove the permission and grants.
func TestMigration00189_KharchaReadGrant(t *testing.T) {
	name := fmt.Sprintf("test_mig_00189_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.UpTo(db, "migrations", 189))

	countForRole := func(roleID int64) int {
		var n int
		require.NoError(t, db.QueryRow(`
			SELECT COUNT(*)
			FROM role_permissions rp
			JOIN permissions p ON p.id = rp.permission_id
			WHERE p.name = 'kharcha:read' AND rp.role_id = ?`, roleID).Scan(&n))
		return n
	}

	for _, roleID := range []int64{1, 2, 3, 4, 6} {
		require.Equal(t, 1, countForRole(roleID),
			"role %d must hold kharcha:read", roleID)
	}
	require.Equal(t, 0, countForRole(5), "driver must NOT hold kharcha:read")

	require.NoError(t, goose.DownTo(db, "migrations", 188))
	var n int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM permissions WHERE name = 'kharcha:read'`).Scan(&n))
	require.Equal(t, 0, n, "down must remove the permission row")
	for _, roleID := range []int64{1, 2, 3, 4, 6} {
		require.Equal(t, 0, countForRole(roleID),
			"down must remove the role %d grant", roleID)
	}

	require.NoError(t, goose.Up(db, "migrations"))
	require.Equal(t, 1, countForRole(1), "up must re-apply cleanly")
}
