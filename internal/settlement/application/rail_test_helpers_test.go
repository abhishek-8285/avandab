package application_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	"transport-app/internal/secure"
	"transport-app/internal/settlement/application"
	"transport-app/internal/settlement/payout"
)

// testPayoutKey is a throwaway AES-256 key (base64 of 32 bytes) for tests only.
const testPayoutKey = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="

// seedEncryptedAccount stores a real AES-GCM ciphertext so the payout rail can
// decrypt the account exactly like production rows.
func seedEncryptedAccount(t *testing.T, db *sql.DB, accountID, holder, accNum, ifsc string) {
	t.Helper()
	t.Setenv("PAYOUT_ENCRYPTION_KEY", testPayoutKey)
	enc, err := secure.EncryptAccountNumber(accNum)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE driver_payout_accounts
		SET account_holder_name = ?, account_number_encrypted = ?, account_number_masked = ?,
		    ifsc_code = ?, bank_name = ?
		WHERE id = ?`, holder, enc, "XXXX"+accNum[len(accNum)-4:], ifsc, "Test Bank", accountID)
	require.NoError(t, err)
}

// fakeRailProvider succeeds immediately; tests that need failures replace
// createFn.
type fakeRailProvider struct {
	createFn func(payout.CreateRequest) (*payout.CreateResult, error)
	verifyFn func(payout.VerifyRequest) (*payout.VerifyResult, error)
}

func (f *fakeRailProvider) CreatePayout(_ context.Context, req payout.CreateRequest) (*payout.CreateResult, error) {
	if f.createFn != nil {
		return f.createFn(req)
	}
	// No provider id: webhook tests assert their own provider id and the
	// mismatch guard only applies when a stored id exists.
	return &payout.CreateResult{Status: payout.StatusProcessing}, nil
}

func (f *fakeRailProvider) VerifyBankAccount(_ context.Context, req payout.VerifyRequest) (*payout.VerifyResult, error) {
	if f.verifyFn != nil {
		return f.verifyFn(req)
	}
	return &payout.VerifyResult{FundAccountID: "fa_fake_1", Status: "created"}, nil
}

// enableFakePayoutRail wires a successful fake provider + enabled rail.
func enableFakePayoutRail(t *testing.T, svc *application.SettlementAppService) {
	t.Helper()
	svc.SetPayoutProvider(&fakeRailProvider{})
	svc.SetPayoutRailEnabled(true)
}
