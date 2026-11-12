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

func TestMigration00169_ProviderModeProvenance_UpAndDown(t *testing.T) {
	name := fmt.Sprintf("test_mig_00169_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.UpTo(db, "migrations", 168))
	require.NoError(t, goose.UpTo(db, "migrations", 169))

	// Verify columns exist on all 3 tables
	for _, tbl := range []string{"eway_bills", "fastag_transactions", "driver_expenses"} {
		var colCount int
		require.NoError(t, db.QueryRow(
			fmt.Sprintf(`SELECT COUNT(*) FROM pragma_table_info('%s') WHERE name='provider_mode'`, tbl)).Scan(&colCount))
		require.Equal(t, 1, colCount, fmt.Sprintf("%s.provider_mode must exist at v169", tbl))
	}

	// Verify default is 'LIVE'
	var defaultVal string
	require.NoError(t, db.QueryRow(
		`SELECT dflt_value FROM pragma_table_info('eway_bills') WHERE name='provider_mode'`).Scan(&defaultVal))
	require.Contains(t, defaultVal, "LIVE")

	// Verify rollback drops the column
	require.NoError(t, goose.DownTo(db, "migrations", 168))
	for _, tbl := range []string{"eway_bills", "fastag_transactions", "driver_expenses"} {
		var colCount int
		require.NoError(t, db.QueryRow(
			fmt.Sprintf(`SELECT COUNT(*) FROM pragma_table_info('%s') WHERE name='provider_mode'`, tbl)).Scan(&colCount))
		require.Equal(t, 0, colCount, fmt.Sprintf("rollback must drop %s.provider_mode", tbl))
	}

	// Re-apply to verify cleanly re-runnable
	require.NoError(t, goose.UpTo(db, "migrations", 169))
}
