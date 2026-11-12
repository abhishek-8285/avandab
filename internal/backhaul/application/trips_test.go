package application

// Focused tests for the extracted backhaul trip selector (handlers-end-state
// ADR, Phase 2): tenant isolation, eligible statuses, ordering.

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

func TestSweepRecentTrips_TenantScopedEligibleStatuses(t *testing.T) {
	name := fmt.Sprintf("sweep_backhaul_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE trips (id TEXT PRIMARY KEY, tenant_id TEXT, trip_number TEXT, status TEXT, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO trips (id, tenant_id, trip_number, status, created_at) VALUES
		('t-old','tenant-a','TRP-OLD','scheduled','2026-10-01 00:00:00'),
		('t-new','tenant-a','TRP-NEW','started','2026-10-02 00:00:00'),
		('t-cancel','tenant-a','TRP-CANCEL','cancelled','2026-10-03 00:00:00'),
		('t-b','tenant-b','TRP-B','assigned','2026-10-04 00:00:00')`)
	require.NoError(t, err)

	trips := RecentTrips(context.Background(), db, "tenant-a")
	require.Len(t, trips, 2, "cancelled excluded, other tenant excluded")
	assert.Equal(t, "t-new", trips[0].ID, "newest first")
	assert.Equal(t, "TRP-NEW", trips[0].TripNumber)
	assert.Equal(t, "t-old", trips[1].ID)
	assert.Nil(t, RecentTrips(context.Background(), db, "tenant-missing"))
	assert.Nil(t, RecentTrips(context.Background(), nil, "tenant-a"))
}
