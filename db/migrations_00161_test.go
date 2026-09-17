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

func TestMigration00161_TenantAccountingSettings_UpAndDown(t *testing.T) {
	name := fmt.Sprintf("test_mig_00161_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.UpTo(db, "migrations", 161))

	_, err = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('t-acc', 'Acc Co', 'acc-co')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO tenant_accounting_settings (tenant_id, provider, endpoint)
		VALUES ('t-acc', 'tally', 'http://localhost:9000')`)
	require.NoError(t, err)

	var provider, endpoint string
	require.NoError(t, db.QueryRow(
		`SELECT provider, endpoint FROM tenant_accounting_settings WHERE tenant_id = 't-acc'`).Scan(&provider, &endpoint))
	require.Equal(t, "tally", provider)
	require.Equal(t, "http://localhost:9000", endpoint)

	// Bad provider violates CHECK; unknown tenant trips the FK trigger.
	_, err = db.Exec(`INSERT INTO tenant_accounting_settings (tenant_id, provider) VALUES ('t-acc', 'sap')`)
	require.Error(t, err, "CHECK must reject unknown provider")
	_, err = db.Exec(`INSERT INTO tenant_accounting_settings (tenant_id, provider) VALUES ('ghost', 'tally')`)
	require.Error(t, err, "FK trigger must reject unknown tenant")

	// Down drops the table; re-up restores it empty.
	require.NoError(t, goose.DownTo(db, "migrations", 160))
	_, err = db.Exec(`SELECT COUNT(*) FROM tenant_accounting_settings`)
	require.Error(t, err, "table must be gone after Down")
	require.NoError(t, goose.UpTo(db, "migrations", 161))
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM tenant_accounting_settings`).Scan(&count))
	require.Equal(t, 0, count)
}
