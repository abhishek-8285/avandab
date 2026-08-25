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

// TestBookingRepository_SearchReadModelsDateRange proves the from/to window
// filters on pickup_date used by the bookings list page calendar.
func TestBookingRepository_SearchReadModelsDateRange(t *testing.T) {
	dbConn := setupBookingTestDB(t)
	repo, ok := NewBookingRepository(dbConn).(interface {
		SearchReadModelsDateRange(ctx context.Context, tenantID shared.TenantID, query string, status string, from string, to string, limit int, offset int) ([]bookingdomain.BookingReadModel, int64, error)
	})
	require.True(t, ok, "booking repo must implement date-range search")
	seedCustomer(t, dbConn, "c-1", "S SHARMA", "")
	seedRoute(t, dbConn, "r-1", "Burari", "Chandani Chowk")

	ctx := context.Background()
	mk := func(id, num string, day int, status string) {
		pickup := time.Date(2026, 8, day, 8, 0, 0, 0, time.UTC)
		_, err := dbConn.Exec(`INSERT INTO bookings (id, booking_number, customer_id, pickup_date, route_id, vehicle_type, passengers, price, status, tenant_id) VALUES (?, ?, ?, ?, ?, 'truck', 1, 1500, ?, '1')`,
			id, num, "c-1", pickup, "r-1", status)
		require.NoError(t, err)
	}
	mk("b-1", "BK-AUG01", 1, "confirmed")
	mk("b-2", "BK-AUG15", 15, "confirmed")
	mk("b-3", "BK-AUG31", 31, "cancelled")

	sepPickup := time.Date(2026, 9, 5, 8, 0, 0, 0, time.UTC)
	_, err := dbConn.Exec(`INSERT INTO bookings (id, booking_number, customer_id, pickup_date, route_id, vehicle_type, passengers, price, status, tenant_id) VALUES ('b-4', 'BK-SEP05', 'c-1', ?, 'r-1', 'truck', 1, 1500, 'confirmed', '1')`, sepPickup)
	require.NoError(t, err)

	rows, total, err := repo.SearchReadModelsDateRange(ctx, "1", "", "", "2026-08-01", "2026-08-31", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 3, total)
	assert.Len(t, rows, 3)
	for _, r := range rows {
		assert.Contains(t, []string{"BK-AUG01", "BK-AUG15", "BK-AUG31"}, r.BookingNumber)
	}

	// Single-day window (A == B)
	rows, total, err = repo.SearchReadModelsDateRange(ctx, "1", "", "", "2026-08-15", "2026-08-15", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	assert.Equal(t, "BK-AUG15", rows[0].BookingNumber)

	// From-only bound includes everything on/after Sep 5
	_, total, err = repo.SearchReadModelsDateRange(ctx, "1", "", "", "2026-09-01", "", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)

	// To-only bound
	_, total, err = repo.SearchReadModelsDateRange(ctx, "1", "", "", "", "2026-08-10", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)

	// Status + date combined
	_, total, err = repo.SearchReadModelsDateRange(ctx, "1", "", "confirmed", "2026-08-01", "2026-08-31", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 2, total)
}
