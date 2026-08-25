package sql

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/shared"
	tripdomain "transport-app/internal/trip/domain"
)

// TestTripRepository_SearchReadModelsDateRange proves the from/to window
// filters on departure_time used by the trips list page calendar.
func TestTripRepository_SearchReadModelsDateRange(t *testing.T) {
	dbConn := setupTripTestDB(t)
	repo, ok := NewTripRepository(dbConn).(interface {
		SearchReadModelsDateRange(ctx context.Context, tenantID shared.TenantID, query string, status string, from string, to string, limit int, offset int) ([]tripdomain.TripReadModel, int64, error)
	})
	require.True(t, ok, "trip repo must implement date-range search")
	seedRoute(t, dbConn, "route-1", "Burari", "Chandani Chowk")

	ctx := context.Background()
	mk := func(id, num string, day int, status string) {
		dep := time.Date(2026, 8, day, 8, 0, 0, 0, time.UTC)
		_, err := dbConn.Exec(`INSERT INTO trips (id, trip_number, route_id, departure_time, status, tenant_id, version) VALUES (?, ?, ?, ?, ?, '1', 1)`,
			id, num, "route-1", dep, status)
		require.NoError(t, err)
	}
	mk("tr-1", "TR-AUG01", 1, "draft")
	mk("tr-2", "TR-AUG10", 10, "started")
	mk("tr-3", "TR-AUG20", 20, "delivered")
	mk("tr-4", "TR-SEP05", 5, "draft")

	// Sep trips need a different month: insert with September date
	_, err := dbConn.Exec(`UPDATE trips SET departure_time = ? WHERE id = 'tr-4'`, time.Date(2026, 9, 5, 8, 0, 0, 0, time.UTC))
	require.NoError(t, err)

	rows, total, err := repo.SearchReadModelsDateRange(ctx, "1", "", "", "2026-08-01", "2026-08-31", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 3, total)
	assert.Len(t, rows, 3)
	for _, r := range rows {
		assert.Contains(t, []string{"TR-AUG01", "TR-AUG10", "TR-AUG20"}, r.TripNumber)
	}

	// Single-day window (A == B)
	rows, total, err = repo.SearchReadModelsDateRange(ctx, "1", "", "", "2026-08-10", "2026-08-10", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	assert.Equal(t, "TR-AUG10", rows[0].TripNumber)

	// From-only bound
	_, total, err = repo.SearchReadModelsDateRange(ctx, "1", "", "", "2026-09-01", "", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)

	// To-only bound
	_, total, err = repo.SearchReadModelsDateRange(ctx, "1", "", "", "", "2026-08-05", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)

	// Status + date combined
	_, total, err = repo.SearchReadModelsDateRange(ctx, "1", "", "draft", "2026-08-01", "2026-08-31", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
}
