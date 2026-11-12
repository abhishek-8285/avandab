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

func TestMigration00178_VideoTelematics_UpAndDown(t *testing.T) {
	name := fmt.Sprintf("test_mig_00178_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.UpTo(db, "migrations", 177))

	// Verify tables do not exist prior to 178
	var count int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='vehicle_dashcam_profiles'`).Scan(&count))
	assert.Equal(t, 0, count, "vehicle_dashcam_profiles table must not exist before v178")

	// Apply migration 00178
	require.NoError(t, goose.UpTo(db, "migrations", 178))

	// Verify tables created
	expectedTables := []string{
		"vehicle_dashcam_profiles",
		"video_safety_events",
		"video_livestream_sessions",
		"driver_safety_scorecards",
	}
	for _, tbl := range expectedTables {
		require.NoError(t, db.QueryRow(
			fmt.Sprintf(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='%s'`, tbl)).Scan(&count))
		assert.Equal(t, 1, count, "table %s must exist after v178", tbl)
	}

	// Verify permissions seeded
	var permCount int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM permissions WHERE name IN ('video:read', 'video:write')`).Scan(&permCount))
	assert.Equal(t, 2, permCount, "video:read and video:write must be seeded in permissions")

	// Verify role_permissions seeded for roles 1, 2, 3, 6
	var rolePermCount int
	require.NoError(t, db.QueryRow(`
		SELECT COUNT(*) FROM role_permissions rp
		JOIN permissions p ON p.id = rp.permission_id
		WHERE p.name IN ('video:read', 'video:write') AND rp.role_id IN (1, 2, 3, 6)
	`).Scan(&rolePermCount))
	assert.Equal(t, 8, rolePermCount, "each role (1, 2, 3, 6) must receive video:read and video:write (8 total)")

	// Test migration down
	require.NoError(t, goose.DownTo(db, "migrations", 177))
	for _, tbl := range expectedTables {
		require.NoError(t, db.QueryRow(
			fmt.Sprintf(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='%s'`, tbl)).Scan(&count))
		assert.Equal(t, 0, count, "table %s must be dropped after down v178", tbl)
	}

	// Verify re-up succeeds cleanly
	require.NoError(t, goose.UpTo(db, "migrations", 178))
}
