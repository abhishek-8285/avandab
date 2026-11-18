package application_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/settlement/application"
	settleSQL "transport-app/internal/settlement/infrastructure/persistence/sql"
)

// Fix #20: when deductions exceed the gross, net payout is clamped to 0 but
// the ledger must not be driven negative by unconditional debit lines.
func TestSettlement_NegativeNetLedgerNeverNegative(t *testing.T) {
	db := setupSettlementTestDB(t)
	repo := settleSQL.NewSQLSettlementRepository(db)
	svc := application.NewSettlementAppService(repo, "secret", 100.0)
	ctx := context.Background()

	_, err := svc.CalculateAndCreateSettlement(ctx, "tenant-1", application.CalculateSettlementRequest{
		TripID: "trip-neg-20", DriverID: "drv-settle-1", GrossFare: 100, AdvanceDeductions: 200,
	})
	require.NoError(t, err)

	var net float64
	require.NoError(t, db.QueryRow(`SELECT net_payout FROM driver_settlements WHERE trip_id='trip-neg-20'`).Scan(&net))
	assert.Equal(t, 0.0, net, "header net payout must be clamped to zero")

	var bal float64
	err = db.QueryRow(`SELECT balance_after FROM driver_ledger_entries
		WHERE tenant_id='tenant-1' AND driver_id='drv-settle-1'
		ORDER BY created_at DESC, rowid DESC LIMIT 1`).Scan(&bal)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, bal, 0.0, "ledger must never drive the wallet negative")
	assert.Equal(t, 0.0, bal, "wallet must match the clamped net payout")
}
