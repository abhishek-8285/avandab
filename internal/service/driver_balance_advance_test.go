package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Fix #8: advances are already netted into a paid settlement's net_payout.
// Subtracting the paid-out advance again double-deducts it from the wallet.
func TestDriverBalance_AdvanceFundedSettlement_NoDoubleDeduct(t *testing.T) {
	db := newVerifyTestDB(t)
	driverID := "drv-adv-bal-8"

	// Gross 1000, approved advance 200 already netted into net_payout 800.
	_, err := db.Exec(`INSERT INTO driver_settlements
		(id, trip_id, driver_id, gross_fare, advances_kharcha, net_payout, status, created_at, paid_at)
		VALUES ('stl-adv-bal-8', 'trip-adv-bal-8', ?, 1000, 200, 800, 'paid', datetime('now'), datetime('now'))`, driverID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO driver_advance_requests
		(id, tenant_id, driver_id, trip_id, amount, reason, status, requested_at)
		VALUES ('adv-adv-bal-8', '1', ?, 'trip-adv-bal-8', 200, 'fuel', 'paid', datetime('now'))`, driverID)
	require.NoError(t, err)

	svc := NewDriverBalanceService(db, nil)
	bal, err := svc.GetBalance(context.Background(), driverID)
	require.NoError(t, err)
	assert.Equal(t, 800.0, bal.RunningBalance, "advance must be deducted once (inside net_payout), not twice")
}
