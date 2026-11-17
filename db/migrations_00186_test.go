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

// TestMigration00186_KharchaApproveGrant proves admin and org_admin hold
// kharcha:approve while dispatcher and driver do not — approval is a
// money-authorization step (dispatcher is denied by the RBAC ratchet; drivers
// must never approve expenses, their own or anyone else's).
func TestMigration00186_KharchaApproveGrant(t *testing.T) {
	name := fmt.Sprintf("test_mig_00186_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.UpTo(db, "migrations", 186))

	countForRole := func(roleID int64) int {
		var n int
		require.NoError(t, db.QueryRow(`
			SELECT COUNT(*)
			FROM role_permissions rp
			JOIN permissions p ON p.id = rp.permission_id
			WHERE p.name = 'kharcha:approve' AND rp.role_id = ?`, roleID).Scan(&n))
		return n
	}

	require.Equal(t, 1, countForRole(1), "admin must hold kharcha:approve (00186 seed)")
	require.Equal(t, 0, countForRole(2), "dispatcher must NOT hold kharcha:approve (RBAC ratchet)")
	require.Equal(t, 1, countForRole(6), "org_admin must hold kharcha:approve (00150)")
	require.Equal(t, 0, countForRole(5), "driver must NOT hold kharcha:approve")

	require.NoError(t, goose.DownTo(db, "migrations", 185))
	require.Equal(t, 0, countForRole(2), "dispatcher must stay denied after down")
	require.Equal(t, 1, countForRole(1), "down must not touch the pre-existing admin grant")
	require.Equal(t, 1, countForRole(6), "down must not touch the pre-existing org_admin grant")
}
