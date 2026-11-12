package db_test

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestMigration00185_DriverKYCRetention_UpAndDown(t *testing.T) {
	name := fmt.Sprintf("test_mig_00185_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.UpTo(db, "migrations", 184))

	hasColumn := func(col string) bool {
		rows, err := db.Query(`PRAGMA table_info(drivers)`)
		require.NoError(t, err)
		defer rows.Close()
		for rows.Next() {
			var cid int
			var name, typ string
			var notnull, pk int
			var dflt sql.NullString
			require.NoError(t, rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk))
			if name == col {
				return true
			}
		}
		return false
	}
	assert.False(t, hasColumn("deactivated_at"), "deactivated_at must not exist before v185")
	assert.False(t, hasColumn("kyc_purged_at"), "kyc_purged_at must not exist before v185")

	old := "2020-01-01 00:00:00"
	_, err = db.Exec(`
		INSERT INTO drivers (id, driver_id, first_name, last_name, phone, status, tenant_id, updated_at)
		VALUES ('drv-old-inactive', 'DRVOLD', 'Old', 'Driver', '9000000000', 'inactive', '1', ?),
		       ('drv-active', 'DRVACT', 'Active', 'Driver', '9000000001', 'available', '1', ?)`, old, old)
	require.NoError(t, err)

	require.NoError(t, goose.UpTo(db, "migrations", 185))
	assert.True(t, hasColumn("deactivated_at"))
	assert.True(t, hasColumn("kyc_purged_at"))

	var deactivated sql.NullString
	require.NoError(t, db.QueryRow(`SELECT deactivated_at FROM drivers WHERE id = 'drv-old-inactive'`).Scan(&deactivated))
	assert.True(t, deactivated.Valid, "existing inactive driver must be backfilled")
	assert.Contains(t, deactivated.String, "2020-01-01")
	require.NoError(t, db.QueryRow(`SELECT deactivated_at FROM drivers WHERE id = 'drv-active'`).Scan(&deactivated))
	assert.False(t, deactivated.Valid, "active driver must stay NULL")

	require.NoError(t, goose.DownTo(db, "migrations", 184))
	assert.False(t, hasColumn("deactivated_at"), "down migration must drop deactivated_at")
	assert.False(t, hasColumn("kyc_purged_at"), "down migration must drop kyc_purged_at")
}
