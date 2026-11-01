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

func TestMigration00176_EVTelematics_UpAndDown(t *testing.T) {
	name := fmt.Sprintf("test_mig_00176_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.UpTo(db, "migrations", 175))

	// Verify tables do not exist prior to 176
	var count int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='vehicle_ev_profiles'`).Scan(&count))
	assert.Equal(t, 0, count, "vehicle_ev_profiles table must not exist before v176")

	// Apply migration 00176
	require.NoError(t, goose.UpTo(db, "migrations", 176))

	// Verify tables created
	expectedTables := []string{
		"vehicle_ev_profiles",
		"vehicle_ev_telemetry_latest",
		"ev_charging_sessions",
		"ev_energy_summaries",
	}
	for _, tbl := range expectedTables {
		require.NoError(t, db.QueryRow(
			fmt.Sprintf(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='%s'`, tbl)).Scan(&count))
		assert.Equal(t, 1, count, "table %s must exist after v176", tbl)
	}

	// Verify permissions seeded
	var permCount int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM permissions WHERE name IN ('ev:read', 'ev:write')`).Scan(&permCount))
	assert.Equal(t, 2, permCount, "ev:read and ev:write must be seeded in permissions")

	// Verify role_permissions seeded for roles 1, 2, 3, 6
	var rolePermCount int
	require.NoError(t, db.QueryRow(`
		SELECT COUNT(*) FROM role_permissions rp
		JOIN permissions p ON p.id = rp.permission_id
		WHERE p.name IN ('ev:read', 'ev:write') AND rp.role_id IN (1, 2, 3, 6)
	`).Scan(&rolePermCount))
	assert.Equal(t, 8, rolePermCount, "each role (1, 2, 3, 6) must receive ev:read and ev:write (8 total)")

	// Test migration down
	require.NoError(t, goose.DownTo(db, "migrations", 175))
	for _, tbl := range expectedTables {
		require.NoError(t, db.QueryRow(
			fmt.Sprintf(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='%s'`, tbl)).Scan(&count))
		assert.Equal(t, 0, count, "table %s must be dropped after rolling back v176", tbl)
	}

	// Re-apply to 176 to ensure re-runnability
	require.NoError(t, goose.UpTo(db, "migrations", 176))
}
