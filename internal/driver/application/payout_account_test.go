package application_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/driver/application"
	"transport-app/internal/secure"
)

func testPayoutKey() string {
	return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x42}, 32))
}

// Red-first: account numbers must be encrypted at rest. Before the fix the raw
// number was written straight into account_number_encrypted.
func TestSubmitPayoutAccount_EncryptsAccountNumber(t *testing.T) {
	db := setupAppServiceTestDB(t)
	svc := application.NewDriverAppService(db)
	ctx := context.Background()

	const accNum = "123456789012"
	t.Setenv("PAYOUT_ENCRYPTION_KEY", testPayoutKey())

	accID, err := svc.SubmitPayoutAccount(ctx, "tenant-1", "drv-enc", "Rajesh Kumar", accNum, "SBIN0001234", "State Bank of India")
	require.NoError(t, err)

	var stored, masked string
	require.NoError(t, db.QueryRow(
		`SELECT account_number_encrypted, account_number_masked FROM driver_payout_accounts WHERE id = ?`, accID,
	).Scan(&stored, &masked))
	assert.NotEqual(t, accNum, stored, "account number must not be stored in plaintext")
	assert.NotContains(t, stored, accNum)
	assert.Equal(t, "******9012", masked)

	plain, err := secure.DecryptAccountNumber(stored)
	require.NoError(t, err)
	assert.Equal(t, accNum, plain, "stored ciphertext must round-trip")

	// Missing key fails closed: error surfaces and no row is inserted.
	t.Setenv("PAYOUT_ENCRYPTION_KEY", "")
	_, err = svc.SubmitPayoutAccount(ctx, "tenant-1", "drv-enc", "Rajesh Kumar", "999988887777", "SBIN0001234", "State Bank of India")
	require.Error(t, err)
	assert.True(t, errors.Is(err, secure.ErrPayoutKeyNotConfigured),
		"want ErrPayoutKeyNotConfigured, got %v", err)

	var count int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM driver_payout_accounts WHERE tenant_id = 'tenant-1' AND driver_id = 'drv-enc'`,
	).Scan(&count))
	assert.Equal(t, 1, count, "failed encryption must not insert a row")
}
