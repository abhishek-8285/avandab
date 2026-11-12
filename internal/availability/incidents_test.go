package availability

// Focused tests for the extracted status-page incident feed (handlers-end-
// state ADR, Phase 2): availability-only filter, newest-first and 10-minute
// de-duplication.

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

func TestSweepLoadIncidents_FilterOrderAndDedup(t *testing.T) {
	name := fmt.Sprintf("sweep_incidents_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE alerts (id TEXT PRIMARY KEY, source TEXT, alert_type TEXT, title TEXT, message TEXT,
		status TEXT, first_seen_at DATETIME, resolved_at DATETIME)`)
	require.NoError(t, err)

	// Two rows in the same 10-minute bucket de-dupe; another source is ignored.
	_, err = db.Exec(`INSERT INTO alerts VALUES
		('a-1','availability','sla_breach','Breach','down','open','2026-10-01 10:01:00',NULL),
		('a-2','availability','sla_breach','Breach dup','down','open','2026-10-01 10:02:00',NULL),
		('a-3','availability','sla_breach','Breach newer','down','resolved','2026-10-01 11:00:00','2026-10-01 11:30:00'),
		('a-4','telemetry','sla_breach','Other source','down','open','2026-10-01 12:00:00',NULL)`)
	require.NoError(t, err)

	incidents := LoadIncidents(context.Background(), db)
	require.Len(t, incidents, 2, "same-bucket duplicate + other source dropped")
	assert.Equal(t, "Breach newer", incidents[0].Title, "newest first")
	assert.True(t, incidents[0].Resolved.Valid)
	assert.Equal(t, "Breach dup", incidents[1].Title, "first row in the 10-minute bucket wins (newest in bucket)")

	assert.Nil(t, LoadIncidents(context.Background(), nil))
}
