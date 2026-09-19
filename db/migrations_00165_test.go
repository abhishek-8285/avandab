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

func TestMigration00165_AuditLogsLocation_UpAndDown(t *testing.T) {
	name := fmt.Sprintf("test_mig_00165_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.UpTo(db, "migrations", 164))
	require.NoError(t, goose.UpTo(db, "migrations", 165))

	var colCount int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('audit_logs') WHERE name='location'`).Scan(&colCount))
	require.Equal(t, 1, colCount, "audit_logs.location must exist at v165")

	_, err = db.Exec(`INSERT INTO audit_logs (id, action, table_name, ip_address, location)
		VALUES ('loc-probe', 'login', 'users', '203.0.113.9', 'Pune, IN')`)
	require.NoError(t, err, "login audit with IP+location must insert at v165")

	require.NoError(t, goose.DownTo(db, "migrations", 164))
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('audit_logs') WHERE name='location'`).Scan(&colCount))
	require.Equal(t, 0, colCount, "rollback must drop audit_logs.location")
	require.NoError(t, goose.UpTo(db, "migrations", 165))
}
