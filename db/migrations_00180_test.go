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

func TestMigration00180_AvailabilitySamples_UpAndDown(t *testing.T) {
	name := fmt.Sprintf("test_mig_00180_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.UpTo(db, "migrations", 179))

	var count int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='availability_samples'`).Scan(&count))
	assert.Equal(t, 0, count, "availability_samples table must not exist before v180")

	require.NoError(t, goose.UpTo(db, "migrations", 180))

	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='availability_samples'`).Scan(&count))
	assert.Equal(t, 1, count, "availability_samples table must exist after v180")

	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_availability_samples_sampled_at'`).Scan(&count))
	assert.Equal(t, 1, count, "idx_availability_samples_sampled_at must exist after v180")

	_, err = db.Exec(`
		INSERT INTO availability_samples (sampled_at, component, healthy, latency_ms)
		VALUES (datetime('now'), 'overall', 1, 2)
	`)
	require.NoError(t, err, "insert into availability_samples must succeed")

	require.NoError(t, goose.DownTo(db, "migrations", 179))
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='availability_samples'`).Scan(&count))
	assert.Equal(t, 0, count, "availability_samples table must be dropped on down migration")
}
