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

func TestMigration00174_ReportBuilder_UpAndDown(t *testing.T) {
	name := fmt.Sprintf("test_mig_00174_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.UpTo(db, "migrations", 173))

	// Verify custom_reports does not exist prior to 174
	var count int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='custom_reports'`).Scan(&count))
	assert.Equal(t, 0, count, "custom_reports table must not exist before v174")

	// Apply migration 00174
	require.NoError(t, goose.UpTo(db, "migrations", 174))

	// Verify tables created
	expectedTables := []string{
		"custom_reports",
		"report_schedules",
		"report_executions",
	}
	for _, tbl := range expectedTables {
		require.NoError(t, db.QueryRow(
			fmt.Sprintf(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='%s'`, tbl)).Scan(&count))
		assert.Equal(t, 1, count, "table %s must exist after v174", tbl)
	}

	// Verify permissions seeded
	var permCount int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM permissions WHERE name = 'reports:write'`).Scan(&permCount))
	assert.Equal(t, 1, permCount, "reports:write must be seeded in permissions")

	// Verify role_permissions seeded for roles 1, 2, 6
	var rolePermCount int
	require.NoError(t, db.QueryRow(`
		SELECT COUNT(*) FROM role_permissions rp
		JOIN permissions p ON p.id = rp.permission_id
		WHERE p.name = 'reports:write' AND rp.role_id IN (1, 2, 6)
	`).Scan(&rolePermCount))
	assert.Equal(t, 3, rolePermCount, "each role (1, 2, 6) must receive reports:write")

	// Verify inserting and updating records with tenant trigger
	_, err = db.Exec(`INSERT INTO tenants (id, name, slug) VALUES ('tenant-174', 'Tenant 174', 't-174')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO custom_reports (
		id, tenant_id, name, description, dataset, selected_columns, is_system
	) VALUES (
		'cr-1', 'tenant-174', 'Trip Distance Summary', 'Reports on trip completion and distance', 'TRIPS', '["trip_number","status","distance"]', 0
	)`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO report_schedules (
		id, tenant_id, report_id, name, frequency, cron_expression, format, channel, recipients
	) VALUES (
		'rs-1', 'tenant-174', 'cr-1', 'Daily Morning Trip Report', 'DAILY', '0 8 * * *', 'CSV', 'EMAIL', '["fleetops@example.com"]'
	)`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO report_executions (
		id, tenant_id, report_id, schedule_id, format, status, row_count, file_size_bytes, triggered_by
	) VALUES (
		're-1', 'tenant-174', 'cr-1', 'rs-1', 'CSV', 'COMPLETED', 45, 2048, 'SCHEDULE'
	)`)
	require.NoError(t, err)

	// Roll back migration 00174
	require.NoError(t, goose.DownTo(db, "migrations", 173))

	for _, tbl := range expectedTables {
		require.NoError(t, db.QueryRow(
			fmt.Sprintf(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='%s'`, tbl)).Scan(&count))
		assert.Equal(t, 0, count, "table %s must be dropped after down migration", tbl)
	}

	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM permissions WHERE name = 'reports:write'`).Scan(&permCount))
	assert.Equal(t, 0, permCount, "permissions must be deleted on rollback")
}
