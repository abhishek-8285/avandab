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

// TestTripRepository_SearchActiveFilter proves the "active" pseudo-status
// (dashboard Active Trips drill-down → /trips?status=active) returns every
// non-terminal, non-draft trip and nothing else.
func TestTripRepository_SearchActiveFilter(t *testing.T) {
	dbConn := setupTripTestDB(t)
	repo := NewTripRepository(dbConn)
	seedRoute(t, dbConn, "route-1", "Burari", "Chandani Chowk")

	ctx := context.Background()
	dep := time.Date(2026, 8, 10, 8, 0, 0, 0, time.UTC)
	statuses := []struct{ id, num, status string }{
		{"tr-draft", "TR-DRAFT", "draft"},
		{"tr-sched", "TR-SCHED", "scheduled"},
		{"tr-assign", "TR-ASSIGN", "assigned"},
		{"tr-start", "TR-START", "started"},
		{"tr-pickup", "TR-PICKUP", "reached_pickup"},
		{"tr-transit", "TR-TRANSIT", "in_transit"},
		{"tr-deliv", "TR-DELIV", "delivered"},
		{"tr-done", "TR-DONE", "completed"},
		{"tr-cancel", "TR-CANCEL", "cancelled"},
	}
	for _, s := range statuses {
		_, err := dbConn.Exec(`INSERT INTO trips (id, trip_number, route_id, departure_time, status, tenant_id, version) VALUES (?, ?, ?, ?, ?, '1', 1)`,
			s.id, s.num, "route-1", dep, s.status)
		require.NoError(t, err)
	}

	// Same-tenant isolation: another tenant's active trip must not leak in.
	_, err := dbConn.Exec(`INSERT INTO trips (id, trip_number, route_id, departure_time, status, tenant_id, version) VALUES ('tr-other', 'TR-OTHER', 'route-1', ?, 'started', '2', 1)`, dep)
	require.NoError(t, err)

	rows, total, err := repo.SearchReadModels(ctx, "1", "", "active", 20, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 6, total, "active = scheduled+assigned+started+reached_pickup+in_transit+delivered")
	assert.Len(t, rows, 6)
	for _, r := range rows {
		assert.Contains(t, []string{"TR-SCHED", "TR-ASSIGN", "TR-START", "TR-PICKUP", "TR-TRANSIT", "TR-DELIV"}, r.TripNumber)
	}

	// Unfiltered still returns everything for the tenant.
	_, total, err = repo.SearchReadModels(ctx, "1", "", "", 20, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 9, total)

	// Unknown status matches nothing (same as before the predicate change).
	_, total, err = repo.SearchReadModels(ctx, "1", "", "bogus", 20, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 0, total)

	// Date-range variant honors "active" too.
	dateRepo, ok := repo.(interface {
		SearchReadModelsDateRange(ctx context.Context, tenantID shared.TenantID, query string, status string, from string, to string, limit int, offset int) ([]tripdomain.TripReadModel, int64, error)
	})
	require.True(t, ok)
	_, total, err = dateRepo.SearchReadModelsDateRange(ctx, "1", "", "active", "2026-08-01", "2026-08-31", 20, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 6, total)
}
