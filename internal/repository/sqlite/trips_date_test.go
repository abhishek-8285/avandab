package sqlite

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/shared"
)

// TestTripsByDate_MatchesTodayTrips is a regression test: the dashboard's
// Today/Active/Completed/Cancelled cards and Upcoming Trips all filter on
// date(departure_time). The date argument must be bound as YYYY-MM-DD text —
// a full timestamp never equals date(col) and silently zeroes the board.
func TestTripsByDate_MatchesTodayTrips(t *testing.T) {
	dbConn, err := sql.Open("sqlite", "file:trips_by_date_repro?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = dbConn.Close() })
	_, err = dbConn.Exec(`CREATE TABLE trips (
		id TEXT PRIMARY KEY, trip_number TEXT NOT NULL, booking_id TEXT,
		driver_id TEXT, vehicle_id TEXT, route_id TEXT NOT NULL,
		departure_time DATETIME NOT NULL, arrival_time DATETIME,
		status TEXT NOT NULL, remarks TEXT, tenant_id TEXT NOT NULL,
		version INTEGER NOT NULL DEFAULT 1,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		started_at DATETIME, reached_pickup_at DATETIME,
		in_transit_at DATETIME, delivered_at DATETIME, completed_at DATETIME
	)`)
	require.NoError(t, err)

	today := time.Now().Format("2006-01-02")
	// Stored exactly as the app writes departures ("2006-01-02 15:04:05").
	_, err = dbConn.Exec(`INSERT INTO trips (id, trip_number, route_id, departure_time, status, tenant_id, version)
		VALUES ('tr-today', 'TR-TODAY', 'route-1', ?, 'assigned', '1', 1)`, today+" 06:00:00")
	require.NoError(t, err)

	repo := NewRepository(dbConn)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	counts, err := repo.CountTripsByStatusForDate(ctx, today)
	require.NoError(t, err)
	assert.EqualValues(t, 1, counts["assigned"], "trip departing today must be counted")

	// Other tenant must not leak in.
	ctx2 := shared.ContextWithTenantID(context.Background(), "2")
	counts2, err := repo.CountTripsByStatusForDate(ctx2, today)
	require.NoError(t, err)
	assert.Empty(t, counts2)
}
