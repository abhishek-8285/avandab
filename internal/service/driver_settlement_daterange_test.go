package service_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDriverSettlementService_ListSettlementsDateRange proves the created_at
// window filter used by the settlements list page calendar. Rows are seeded
// with RFC3339 created_at values on purpose: the window must use
// date(substr(col,1,10)), not date(col).
func TestDriverSettlementService_ListSettlementsDateRange(t *testing.T) {
	dbConn, svcs, _ := setupComplianceTestDB(t)
	ctx := context.Background()

	mk := func(id, tripID, status string, created string) {
		_, err := dbConn.Exec(`INSERT INTO driver_settlements
			(id, trip_id, driver_id, gross_fare, commission_amount, advances_kharcha,
			 deductions, performance_bonus, tds_rate, tds_amount, net_payout,
			 rate_model, status, created_at, updated_at)
			VALUES (?, ?, 'drv-1', 1000, 100, 0, 0, 0, 0, 0, 900, 'per_km', ?, ?, ?)`,
			id, tripID, status, created, created)
		require.NoError(t, err)
	}
	mk("s1", "trip-1", "pending", "2026-08-01T08:00:00Z")
	mk("s2", "trip-2", "paid", "2026-08-10T08:00:00Z")
	mk("s3", "trip-3", "disputed", "2026-08-20T08:00:00Z")

	// Full-month window
	recs, err := svcs.Settlements.ListSettlementsDateRange(ctx, "", "", "2026-08-01", "2026-08-31", 50, 0)
	require.NoError(t, err)
	assert.Len(t, recs, 3)

	// Single-day window (from == to)
	recs, err = svcs.Settlements.ListSettlementsDateRange(ctx, "", "", "2026-08-10", "2026-08-10", 50, 0)
	require.NoError(t, err)
	require.Len(t, recs, 1)
	assert.Equal(t, "trip-2", string(recs[0].TripID))

	// From-only bound
	recs, err = svcs.Settlements.ListSettlementsDateRange(ctx, "", "", "2026-08-11", "", 50, 0)
	require.NoError(t, err)
	assert.Len(t, recs, 1)

	// To-only bound
	recs, err = svcs.Settlements.ListSettlementsDateRange(ctx, "", "", "", "2026-08-05", 50, 0)
	require.NoError(t, err)
	assert.Len(t, recs, 1)

	// Status + date combined
	recs, err = svcs.Settlements.ListSettlementsDateRange(ctx, "pending", "", "2026-08-01", "2026-08-31", 50, 0)
	require.NoError(t, err)
	require.Len(t, recs, 1)
	assert.Equal(t, "trip-1", string(recs[0].TripID))
}
