package sql

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	bookingdomain "transport-app/internal/booking/domain"
	"transport-app/internal/shared"
)

// TestBookingRepository_SearchUnassignedFilter proves the "unassigned"
// pseudo-status (dashboard exception strip → /bookings?status=unassigned)
// returns confirmed/pending bookings with no trip and nothing else.
func TestBookingRepository_SearchUnassignedFilter(t *testing.T) {
	dbConn := setupBookingTestDB(t)
	repo := NewBookingRepository(dbConn)
	seedCustomer(t, dbConn, "c-1", "S SHARMA", "")
	seedRoute(t, dbConn, "r-1", "Burari", "Chandani Chowk")
	_, err := dbConn.Exec(`CREATE TABLE trips (id TEXT PRIMARY KEY, booking_id TEXT, tenant_id TEXT NOT NULL DEFAULT '1')`)
	require.NoError(t, err)

	ctx := context.Background()
	pickup := time.Date(2026, 8, 10, 8, 0, 0, 0, time.UTC)
	mk := func(id, num, status, tenant string) {
		_, err := dbConn.Exec(`INSERT INTO bookings (id, booking_number, customer_id, pickup_date, route_id, vehicle_type, passengers, price, status, tenant_id) VALUES (?, ?, 'c-1', ?, 'r-1', 'truck', 1, 1500, ?, ?)`,
			id, num, pickup, status, tenant)
		require.NoError(t, err)
	}
	mk("b-pend", "BK-PEND", "pending", "1")
	mk("b-conf", "BK-CONF", "confirmed", "1")
	mk("b-done", "BK-DONE", "completed", "1")
	mk("b-canc", "BK-CANC", "cancelled", "1")
	mk("b-other", "BK-OTHER", "confirmed", "2")

	// b-conf gets a trip → no longer unassigned.
	_, err = dbConn.Exec(`INSERT INTO trips (id, booking_id, tenant_id) VALUES ('tr-1', 'b-conf', '1')`)
	require.NoError(t, err)

	rows, total, err := repo.SearchReadModels(ctx, "1", "", "unassigned", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total, "only pending BK-PEND has no trip")
	require.Len(t, rows, 1)
	assert.Equal(t, "BK-PEND", rows[0].BookingNumber)

	// Exact statuses keep working.
	_, total, err = repo.SearchReadModels(ctx, "1", "", "confirmed", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)

	// Date-range variant honors "unassigned" too.
	dateRepo, ok := repo.(interface {
		SearchReadModelsDateRange(ctx context.Context, tenantID shared.TenantID, query string, status string, from string, to string, limit int, offset int) ([]bookingdomain.BookingReadModel, int64, error)
	})
	require.True(t, ok)
	_, total, err = dateRepo.SearchReadModelsDateRange(ctx, "1", "", "unassigned", "2026-08-01", "2026-08-31", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	_, total, err = dateRepo.SearchReadModelsDateRange(ctx, "1", "", "unassigned", "2026-09-01", "", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 0, total)
}
