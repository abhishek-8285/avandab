package handlers

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	"transport-app/internal/secure"
	"transport-app/internal/settlement/payout"
)

const handlerPayoutKey = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="

func seedHandlerEncryptedAccount(t *testing.T, db *sql.DB) {
	t.Helper()
	t.Setenv("PAYOUT_ENCRYPTION_KEY", handlerPayoutKey)
	enc, err := secure.EncryptAccountNumber("1122334455")
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE driver_payout_accounts
		SET account_holder_name = 'Sunil Sharma', account_number_encrypted = ?,
		    account_number_masked = 'XXXX4455', ifsc_code = 'SBIN0001234', bank_name = 'Test Bank'
		WHERE id = 'acc-1'`, enc)
	require.NoError(t, err)
}

type handlerFakePayoutProvider struct{}

func (h *handlerFakePayoutProvider) CreatePayout(_ context.Context, _ payout.CreateRequest) (*payout.CreateResult, error) {
	return &payout.CreateResult{Status: payout.StatusProcessing}, nil
}

func (h *handlerFakePayoutProvider) VerifyBankAccount(_ context.Context, _ payout.VerifyRequest) (*payout.VerifyResult, error) {
	return &payout.VerifyResult{FundAccountID: "fa_handler_fake", Status: "created"}, nil
}
