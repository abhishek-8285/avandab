package application_test

// Atomicity regression tests for the settlement/payout rail.
//
// Each money movement must be exactly ONE atomic unit. The service currently
// commits its stages as independent transactions (settlement header, then each
// ledger line; payout instruction, then debit; webhook status, then
// compensation, then processed marker). A failure between stages must leave
// NOTHING behind.
//
// Failure is injected deterministically with a SQLite trigger that raises on
// the second write of each unit. These tests are RED against the current
// multi-write code and GREEN once each flow commits as one transaction.

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/settlement/application"
	"transport-app/internal/settlement/domain"
	settleSQL "transport-app/internal/settlement/infrastructure/persistence/sql"
)

func dropTrigger(t *testing.T, db *sql.DB, name string) {
	t.Helper()
	_, err := db.Exec(`DROP TRIGGER IF EXISTS ` + name)
	require.NoError(t, err)
}

func signWebhook(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return hex.EncodeToString(mac.Sum(nil))
}

// 1. Settlement header + ledger lines are one unit. Injected failure on the
// second ledger line (COMMISSION) must not leave the header or the first
// line (TRIP_EARNING) committed.
func TestAtomicity_SettlementHeaderAndLedgerAreOneUnit(t *testing.T) {
	db := setupSettlementTestDB(t)
	_, err := db.Exec(`CREATE TRIGGER fail_commission BEFORE INSERT ON driver_ledger_entries
		WHEN NEW.entry_type = 'COMMISSION'
		BEGIN SELECT RAISE(ABORT, 'injected ledger failure'); END`)
	require.NoError(t, err)

	repo := settleSQL.NewSQLSettlementRepository(db)
	svc := application.NewSettlementAppService(repo, "secret_key_123", 100.0)

	_, err = svc.CalculateAndCreateSettlement(context.Background(), "tenant-1", application.CalculateSettlementRequest{
		TripID:            "trip-atomic-1",
		DriverID:          "drv-settle-1",
		GrossFare:         2000.0,
		AdvanceDeductions: 0,
		CommissionRate:    0.10,
		TDSRate:           0.01,
	})
	require.Error(t, err, "injected ledger failure must surface")

	var headers, entries int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM driver_settlements WHERE trip_id = 'trip-atomic-1'`).Scan(&headers))
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM driver_ledger_entries WHERE driver_id = 'drv-settle-1'`).Scan(&entries))

	assert.Equal(t, 0, headers, "settlement header must not survive a failed ledger write")
	assert.Equal(t, 0, entries, "no partial ledger entries may survive a failed settlement")
}

// 2. Payout instruction + wallet debit are one unit. Injected failure on the
// debit must not leave the instruction behind (otherwise a later webhook could
// pay an instruction that was never debited).
func TestAtomicity_PayoutInstructionAndDebitAreOneUnit(t *testing.T) {
	db := setupSettlementTestDB(t)
	repo := settleSQL.NewSQLSettlementRepository(db)
	svc := application.NewSettlementAppService(repo, "secret_key_123", 100.0)
	ctx := context.Background()

	_, err := svc.CalculateAndCreateSettlement(ctx, "tenant-1", application.CalculateSettlementRequest{
		TripID: "trip-atomic-fund", DriverID: "drv-settle-1", GrossFare: 2000.0, CommissionRate: 0.10, TDSRate: 0.01,
	})
	require.NoError(t, err)

	_, err = db.Exec(`CREATE TRIGGER fail_payout_ledger BEFORE INSERT ON driver_ledger_entries
		WHEN NEW.entry_type = 'PAYOUT'
		BEGIN SELECT RAISE(ABORT, 'injected payout debit failure'); END`)
	require.NoError(t, err)

	_, err = svc.InitiatePayout(ctx, "tenant-1", "drv-settle-1", application.InitiatePayoutRequest{
		IdempotencyKey: "idem-atomic-payout",
		Amount:         500.0,
	})
	require.Error(t, err, "injected payout debit failure must surface")

	var instructions, debits int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM payout_instructions WHERE idempotency_key = 'idem-atomic-payout'`).Scan(&instructions))
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM driver_ledger_entries WHERE entry_type = 'PAYOUT'`).Scan(&debits))

	assert.Equal(t, 0, instructions, "payout instruction must not survive a failed debit")
	assert.Equal(t, 0, debits, "no partial payout debit may survive")
}

// 3. The crash-partial from #2 heals on retry: a retry that reports success
// (even as a duplicate) must correspond to exactly one debit and a reduced
// wallet. Current code returns the orphaned instruction as a duplicate and
// never debits — a free payout.
func TestAtomicity_PayoutDuplicateMustNotSkipDebit(t *testing.T) {
	db := setupSettlementTestDB(t)
	repo := settleSQL.NewSQLSettlementRepository(db)
	svc := application.NewSettlementAppService(repo, "secret_key_123", 100.0)
	ctx := context.Background()

	_, err := svc.CalculateAndCreateSettlement(ctx, "tenant-1", application.CalculateSettlementRequest{
		TripID: "trip-atomic-fund2", DriverID: "drv-settle-1", GrossFare: 2000.0, CommissionRate: 0.10, TDSRate: 0.01,
	})
	require.NoError(t, err)

	_, err = db.Exec(`CREATE TRIGGER fail_payout_ledger2 BEFORE INSERT ON driver_ledger_entries
		WHEN NEW.entry_type = 'PAYOUT'
		BEGIN SELECT RAISE(ABORT, 'injected payout debit failure'); END`)
	require.NoError(t, err)

	_, err = svc.InitiatePayout(ctx, "tenant-1", "drv-settle-1", application.InitiatePayoutRequest{
		IdempotencyKey: "idem-dupe-debit",
		Amount:         500.0,
	})
	require.Error(t, err, "first attempt must fail on injected debit error")

	// Operator clears the fault; provider/client retries the same key.
	dropTrigger(t, db, "fail_payout_ledger2")

	resp, err := svc.InitiatePayout(ctx, "tenant-1", "drv-settle-1", application.InitiatePayoutRequest{
		IdempotencyKey: "idem-dupe-debit",
		Amount:         500.0,
	})
	require.NoError(t, err)
	// The failed first attempt rolled back its instruction (see test #2),
	// so the retry is a fresh creation, not a duplicate — the property that
	// matters is that a success response always carries exactly one debit.
	require.False(t, resp.IsDuplicate, "rolled-back retry must create a fresh instruction")

	var debits int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM driver_ledger_entries WHERE entry_type = 'PAYOUT' AND reference_id = ?`,
		resp.PayoutID).Scan(&debits))
	assert.Equal(t, 1, debits, "a successful payout response must have exactly one debit")

	wallet, err := svc.GetDriverWallet(ctx, "tenant-1", "drv-settle-1")
	require.NoError(t, err)
	// 2000 gross - 200 commission - 18 TDS = 1782; once debited, 1282 remains.
	assert.Equal(t, 1282.0, wallet.AvailableBalance,
		"successful duplicate must not leave the wallet undebited")
}

// 4. Webhook status update + compensating credit + processed marker are one
// unit. Injected failure on the compensating credit must not leave the payout
// marked reversed while the driver was never credited.
func TestAtomicity_WebhookStatusAndCompensationAreOneUnit(t *testing.T) {
	db := setupSettlementTestDB(t)
	repo := settleSQL.NewSQLSettlementRepository(db)
	svc := application.NewSettlementAppService(repo, "secret_key_123", 100.0)
	ctx := context.Background()

	_, err := svc.CalculateAndCreateSettlement(ctx, "tenant-1", application.CalculateSettlementRequest{
		TripID: "trip-atomic-rev", DriverID: "drv-settle-1", GrossFare: 2000.0, CommissionRate: 0.10, TDSRate: 0.01,
	})
	require.NoError(t, err)

	payout, err := svc.InitiatePayout(ctx, "tenant-1", "drv-settle-1", application.InitiatePayoutRequest{
		IdempotencyKey: "idem-atomic-rev",
		Amount:         500.0,
	})
	require.NoError(t, err)

	_, err = db.Exec(`CREATE TRIGGER fail_reversal BEFORE INSERT ON driver_ledger_entries
		WHEN NEW.entry_type = 'PAYOUT_REVERSAL'
		BEGIN SELECT RAISE(ABORT, 'injected compensating credit failure'); END`)
	require.NoError(t, err)

	body := fmt.Sprintf(`{
		"event": "payout.reversed",
		"payload": {
			"payout": {
				"entity": {
					"id": "pout_rzp_atomic",
					"amount": 500,
					"currency": "INR",
					"status": "reversed",
					"error_description": "Beneficiary account frozen",
					"reference_id": "%s"
				}
			}
		}
	}`, payout.PayoutID)

	err = svc.ProcessProviderWebhook(ctx, "tenant-1", "evt-atomic-rev", signWebhook("secret_key_123", body), []byte(body))
	require.Error(t, err, "injected compensating credit failure must surface")

	stored, err := repo.GetPayoutByID(ctx, "tenant-1", payout.PayoutID)
	require.NoError(t, err)
	assert.Equal(t, domain.PayoutInitiated, stored.Status,
		"payout must not be marked reversed when the compensating credit failed")

	var compensations, events int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM driver_ledger_entries WHERE reference_type = 'payout_reversal' AND reference_id = ?`,
		payout.PayoutID).Scan(&compensations))
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM provider_events WHERE provider_event_id = 'evt-atomic-rev'`).Scan(&events))
	assert.Equal(t, 0, compensations, "no partial compensation may survive a failed webhook")
	assert.Equal(t, 0, events, "no processed marker may survive a failed webhook")
}
