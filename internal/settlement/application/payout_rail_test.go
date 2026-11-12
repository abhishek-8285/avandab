package application_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/settlement/application"
	"transport-app/internal/settlement/domain"
	settleSQL "transport-app/internal/settlement/infrastructure/persistence/sql"
	"transport-app/internal/settlement/payout"
)

func signPayoutWebhook(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return hex.EncodeToString(mac.Sum(nil))
}

func TestPayoutRail_SuccessRecordsProviderProcessing(t *testing.T) {
	db := setupSettlementTestDB(t)
	repo := settleSQL.NewSQLSettlementRepository(db)
	svc := application.NewSettlementAppService(repo, "secret_key_123", 100.0)
	enableFakePayoutRail(t, svc)
	ctx := context.Background()

	_, err := svc.CalculateAndCreateSettlement(ctx, "tenant-1", application.CalculateSettlementRequest{
		TripID: "trip-rail-ok", DriverID: "drv-settle-1", GrossFare: 2000.0, CommissionRate: 0.10, TDSRate: 0.01,
	})
	require.NoError(t, err)

	resp, err := svc.InitiatePayout(ctx, "tenant-1", "drv-settle-1", application.InitiatePayoutRequest{
		IdempotencyKey: "idem-rail-ok", Amount: 500.0,
	})
	require.NoError(t, err, "rail success must not error")
	assert.Equal(t, domain.PayoutProcessing, resp.Status, "response must reflect the rail dispatch")

	stored, err := repo.GetPayoutByID(ctx, "tenant-1", resp.PayoutID)
	require.NoError(t, err)
	assert.Equal(t, domain.PayoutProcessing, stored.Status)

	var reversals int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM driver_ledger_entries WHERE reference_type = 'payout_reversal' AND reference_id = ?`,
		resp.PayoutID).Scan(&reversals))
	assert.Equal(t, 0, reversals, "successful payout must not be compensated")
}

func TestPayoutRail_ProviderFailureCompensatesWallet(t *testing.T) {
	db := setupSettlementTestDB(t)
	repo := settleSQL.NewSQLSettlementRepository(db)
	svc := application.NewSettlementAppService(repo, "secret_key_123", 100.0)
	svc.SetPayoutProvider(&fakeRailProvider{createFn: func(payout.CreateRequest) (*payout.CreateResult, error) {
		return nil, errors.New("bank rail down")
	}})
	svc.SetPayoutRailEnabled(true)
	ctx := context.Background()

	_, err := svc.CalculateAndCreateSettlement(ctx, "tenant-1", application.CalculateSettlementRequest{
		TripID: "trip-rail-fail", DriverID: "drv-settle-1", GrossFare: 2000.0, CommissionRate: 0.10, TDSRate: 0.01,
	})
	require.NoError(t, err)

	_, err = svc.InitiatePayout(ctx, "tenant-1", "drv-settle-1", application.InitiatePayoutRequest{
		IdempotencyKey: "idem-rail-fail", Amount: 500.0,
	})
	require.Error(t, err, "provider failure must surface")

	var status string
	require.NoError(t, db.QueryRow(`SELECT status FROM payout_instructions WHERE idempotency_key = 'idem-rail-fail'`).Scan(&status))
	assert.Equal(t, "failed", status)

	var reversals int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM driver_ledger_entries WHERE reference_type = 'payout_reversal'`).Scan(&reversals))
	assert.Equal(t, 1, reversals, "exactly one compensating credit")

	wallet, err := svc.GetDriverWallet(ctx, "tenant-1", "drv-settle-1")
	require.NoError(t, err)
	assert.Equal(t, 1782.0, wallet.AvailableBalance, "wallet must be restored after a failed payout")
}

func TestPayoutRail_PennyDropVerificationAndWebhook(t *testing.T) {
	db := setupSettlementTestDB(t)
	repo := settleSQL.NewSQLSettlementRepository(db)
	svc := application.NewSettlementAppService(repo, "secret_key_123", 100.0)
	enableFakePayoutRail(t, svc)
	ctx := context.Background()

	res, err := svc.VerifyPayoutAccount(ctx, "tenant-1", "drv-settle-1")
	require.NoError(t, err)
	assert.Equal(t, "penny_drop_pending", res.Status)
	assert.Equal(t, "fa_fake_1", res.Reference)

	var status, ref string
	require.NoError(t, db.QueryRow(`SELECT verification_status, COALESCE(verification_reference,'') FROM driver_payout_accounts WHERE id = 'acc-1'`).Scan(&status, &ref))
	assert.Equal(t, "penny_drop_pending", status)
	assert.Equal(t, "fa_fake_1", ref)

	body := fmt.Sprintf(`{"event":"fund_account.validation.completed","payload":{"fund_account":{"entity":{"id":"fa_fake_1","status":"completed","results":{"account_status":"valid","registered_name":"Sunil Sharma"}}}}}`)
	sig := signPayoutWebhook("secret_key_123", body)
	require.NoError(t, svc.ProcessProviderWebhook(ctx, "tenant-1", "evt-fa-1", sig, []byte(body)))

	require.NoError(t, db.QueryRow(`SELECT verification_status FROM driver_payout_accounts WHERE id = 'acc-1'`).Scan(&status))
	assert.Equal(t, "verified", status, "valid penny-drop webhook must verify the account")
}
