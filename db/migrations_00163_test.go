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

func TestMigration00163_DropDeadAccountingSeeds_UpAndDown(t *testing.T) {
	name := fmt.Sprintf("test_mig_00163_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.UpTo(db, "migrations", 162))

	// Precondition: the dead 00050 seeds exist before 00163.
	var pre int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM company_config WHERE tenant_id = '1' AND key IN (
		 'accounting_adapter','accounting_enabled','accounting_endpoint','accounting_api_key')`).Scan(&pre))
	require.Equal(t, 4, pre, "dead seeds must exist at v162")

	require.NoError(t, goose.UpTo(db, "migrations", 163))

	var post int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM company_config WHERE tenant_id = '1' AND key IN (
		 'accounting_adapter','accounting_enabled','accounting_endpoint','accounting_api_key')`).Scan(&post))
	require.Equal(t, 0, post, "dead seeds must be gone at v163")

	// Other company_config keys untouched.
	var rest int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM company_config WHERE key NOT IN (
		 'accounting_adapter','accounting_enabled','accounting_endpoint','accounting_api_key')`).Scan(&rest))
	require.Greater(t, rest, 0, "unrelated keys must survive")

	// Rollback restores the seeds; re-up drops them again.
	require.NoError(t, goose.DownTo(db, "migrations", 162))
	var back int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM company_config WHERE tenant_id = '1' AND key IN (
		 'accounting_adapter','accounting_enabled','accounting_endpoint','accounting_api_key')`).Scan(&back))
	require.Equal(t, 4, back, "rollback must restore seeds")
	require.NoError(t, goose.UpTo(db, "migrations", 163))
}
