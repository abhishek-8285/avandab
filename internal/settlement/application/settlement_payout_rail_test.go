package application_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/settlement/application"
	settleSQL "transport-app/internal/settlement/infrastructure/persistence/sql"
)

// Red-first: without a configured payout rail, InitiatePayout must fail closed
// before the idempotency lookup and transaction — no payout instruction, no
// wallet debit. A verified account and sufficient balance are present, so only
// the rail flag stands between this request and a parked debit.
func TestInitiatePayout_RailDisabledFailsClosed(t *testing.T) {
	db := setupSettlementTestDB(t)
	repo := settleSQL.NewSQLSettlementRepository(db)
	svc := application.NewSettlementAppService(repo, "secret_key_123", 100.0)
	ctx := context.Background()

	_, err := svc.CalculateAndCreateSettlement(ctx, "tenant-1", application.CalculateSettlementRequest{
		TripID: "trip-no-rail", DriverID: "drv-settle-1",
		GrossFare: 2000.0, CommissionRate: 0.10, TDSRate: 0.01,
	})
	require.NoError(t, err)

	before, err := svc.GetDriverWallet(ctx, "tenant-1", "drv-settle-1")
	require.NoError(t, err)
	require.Greater(t, before.AvailableBalance, 500.0)

	_, err = svc.InitiatePayout(ctx, "tenant-1", "drv-settle-1", application.InitiatePayoutRequest{
		IdempotencyKey: "idem-no-rail",
		Amount:         500.0,
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, application.ErrPayoutRailDisabled),
		"want ErrPayoutRailDisabled, got %v", err)

	var instructions int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM payout_instructions`).Scan(&instructions))
	assert.Equal(t, 0, instructions, "disabled rail must not write payout_instructions")

	after, err := svc.GetDriverWallet(ctx, "tenant-1", "drv-settle-1")
	require.NoError(t, err)
	assert.Equal(t, before.AvailableBalance, after.AvailableBalance,
		"disabled rail must not debit the driver wallet")
}
