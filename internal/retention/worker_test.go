package retention

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func testRetentionDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("retention_%s_%d", t.Name(), time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	for _, tgt := range DefaultTargets() {
		_, err := db.Exec(fmt.Sprintf(`CREATE TABLE %s (id TEXT PRIMARY KEY, %s DATETIME NOT NULL)`, tgt.Table, tgt.TimeColumn))
		require.NoError(t, err, tgt.Table)
	}
	// KYC purge runs in the same worker pass; give it its tables.
	_, err = db.Exec(`
		CREATE TABLE drivers (id TEXT PRIMARY KEY, deactivated_at DATETIME, kyc_purged_at DATETIME);
		CREATE TABLE driver_documents (id TEXT PRIMARY KEY, driver_id TEXT NOT NULL, file_url TEXT NOT NULL)`)
	require.NoError(t, err, "kyc tables")
	return db
}

func insertRow(t *testing.T, db *sql.DB, table, timeColumn, id string, at time.Time) {
	t.Helper()
	_, err := db.Exec(fmt.Sprintf(`INSERT INTO %s (id, %s) VALUES (?, ?)`, table, timeColumn), id, at.UTC())
	require.NoError(t, err)
}

func countRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&n))
	return n
}

func TestPurgeOnce_DeletesOnlyExpiredRows(t *testing.T) {
	db := testRetentionDB(t)
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		table, column string
		oldAge        time.Duration
	}{
		{"video_safety_events", "event_time", 91 * 24 * time.Hour},
		{"video_livestream_sessions", "started_at", 91 * 24 * time.Hour},
		{"audit_logs", "created_at", 4 * 365 * 24 * time.Hour},
		{"breach_incidents", "detected_at", 4 * 365 * 24 * time.Hour},
		{"user_consents", "created_at", 6 * 365 * 24 * time.Hour},
		{"privacy_dsar_requests", "created_at", 6 * 365 * 24 * time.Hour},
	}
	for _, tc := range cases {
		insertRow(t, db, tc.table, tc.column, "old-"+tc.table, now.Add(-tc.oldAge))
		insertRow(t, db, tc.table, tc.column, "new-"+tc.table, now.Add(-1*time.Hour))
	}

	w := NewWorker(db, nil, time.Hour)
	w.now = func() time.Time { return now }
	counts, err := w.PurgeOnce(context.Background())
	require.NoError(t, err)

	for _, tc := range cases {
		assert.Equal(t, int64(1), counts[tc.table], "%s must purge exactly the expired row", tc.table)
		assert.Equal(t, 1, countRows(t, db, tc.table), "%s must keep the fresh row", tc.table)
	}

	// Idempotent: nothing left to purge.
	counts, err = w.PurgeOnce(context.Background())
	require.NoError(t, err)
	for _, tc := range cases {
		assert.Zero(t, counts[tc.table], "%s second pass must be a no-op", tc.table)
	}
}

func TestPurgeOnce_BatchedDeletesAllExpired(t *testing.T) {
	db := testRetentionDB(t)
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		insertRow(t, db, "video_safety_events", "event_time", fmt.Sprintf("old-%d", i), now.Add(-100*24*time.Hour))
	}
	insertRow(t, db, "video_safety_events", "event_time", "fresh", now.Add(-time.Hour))

	w := NewWorker(db, nil, time.Hour)
	w.batch = 2
	w.now = func() time.Time { return now }
	counts, err := w.PurgeOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(5), counts["video_safety_events"], "batch loop must delete every expired row")
	assert.Equal(t, 1, countRows(t, db, "video_safety_events"))
}
