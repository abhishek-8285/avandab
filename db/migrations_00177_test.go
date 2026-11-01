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

func TestMigration00177_ColdChain_UpAndDown(t *testing.T) {
	name := fmt.Sprintf("test_mig_00177_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.UpTo(db, "migrations", 176))

	// Verify tables do not exist prior to 177
	var count int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='vehicle_reefer_profiles'`).Scan(&count))
	assert.Equal(t, 0, count, "vehicle_reefer_profiles table must not exist before v177")

	// Apply migration 00177
	require.NoError(t, goose.UpTo(db, "migrations", 177))

	// Verify tables created
	expectedTables := []string{
		"vehicle_reefer_profiles",
		"vehicle_reefer_telemetry_latest",
		"reefer_temperature_excursions",
		"reefer_trip_summaries",
	}
	for _, tbl := range expectedTables {
		require.NoError(t, db.QueryRow(
			fmt.Sprintf(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='%s'`, tbl)).Scan(&count))
		assert.Equal(t, 1, count, "table %s must exist after v177", tbl)
	}

	// Verify permissions seeded
	var permCount int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM permissions WHERE name IN ('coldchain:read', 'coldchain:write')`).Scan(&permCount))
	assert.Equal(t, 2, permCount, "coldchain:read and coldchain:write must be seeded in permissions")

	// Verify role_permissions seeded for roles 1, 2, 3, 6
	var rolePermCount int
	require.NoError(t, db.QueryRow(`
		SELECT COUNT(*) FROM role_permissions rp
		JOIN permissions p ON p.id = rp.permission_id
		WHERE p.name IN ('coldchain:read', 'coldchain:write') AND rp.role_id IN (1, 2, 3, 6)
	`).Scan(&rolePermCount))
	assert.Equal(t, 8, rolePermCount, "each role (1, 2, 3, 6) must receive coldchain:read and coldchain:write (8 total)")

	// Test migration down
	require.NoError(t, goose.DownTo(db, "migrations", 176))
	for _, tbl := range expectedTables {
		require.NoError(t, db.QueryRow(
			fmt.Sprintf(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='%s'`, tbl)).Scan(&count))
		assert.Equal(t, 0, count, "table %s must be dropped after rolling back v177", tbl)
	}

	// Re-apply to 177 to ensure re-runnability
	require.NoError(t, goose.UpTo(db, "migrations", 177))
}
