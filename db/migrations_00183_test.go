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

func TestMigration00183_RetentionPurgeIndexes_UpAndDown(t *testing.T) {
	name := fmt.Sprintf("test_mig_00183_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.UpTo(db, "migrations", 182))

	indexes := []string{
		"idx_video_events_event_time",
		"idx_video_livestream_created_at",
		"idx_breach_incidents_detected_at",
		"idx_user_consents_created_at",
		"idx_privacy_dsar_created_at",
	}
	hasIndex := func(name string) int {
		var n int
		require.NoError(t, db.QueryRow(
			`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name = ?`, name).Scan(&n))
		return n
	}
	for _, idx := range indexes {
		assert.Zero(t, hasIndex(idx), "%s must not exist before v183", idx)
	}

	require.NoError(t, goose.UpTo(db, "migrations", 183))
	for _, idx := range indexes {
		assert.Equal(t, 1, hasIndex(idx), "%s must exist after v183", idx)
	}

	require.NoError(t, goose.DownTo(db, "migrations", 182))
	for _, idx := range indexes {
		assert.Zero(t, hasIndex(idx), "%s must be dropped on down migration", idx)
	}
}
