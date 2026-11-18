package pnl

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/shared"
)

// Fare 100, fuel estimate 95 (low 80.75 / high 109.25): the high-bound margin
// is 19.25% but the actual estimate is only 5% — must be flagged low-margin.
func TestCalculate_LowMarginUsesEstimateNotOptimisticBound(t *testing.T) {
	db := newPnLTestDB(t)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	for _, q := range []string{
		`INSERT INTO trips (id, tenant_id, booking_id, vehicle_id) VALUES ('trp_lm', '1', 'bk_lm', 'veh_lm')`,
		`INSERT INTO bookings VALUES ('bk_lm', 100)`,
		`INSERT INTO vehicles VALUES ('veh_lm', 'diesel', 5.0, 5.0)`,
		// 500 km / 5 km/l = 100 L × ₹0.95 = ₹95 estimated fuel.
		`INSERT INTO telemetry_snapshots VALUES ('trp_lm', 100), ('trp_lm', 600)`,
		`INSERT INTO fuel_prices VALUES ('1', 0.95, 0.95, datetime('now'))`,
	} {
		_, err := db.Exec(q)
		require.NoError(t, err)
	}

	p, err := NewService(db).Calculate(ctx, "trp_lm")
	require.NoError(t, err)

	assert.InDelta(t, 95.0, p.FuelCost, 0.001)
	assert.InDelta(t, 80.75, p.FuelCostLow, 0.001)
	assert.InDelta(t, 109.25, p.FuelCostHigh, 0.001)
	assert.InDelta(t, 5.0, p.EstimatedMargin, 0.001)
	assert.True(t, p.LowMargin, "5%% estimated margin must be flagged low-margin")
}
