package secure_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/secure"
)

func key32() string {
	return base64.StdEncoding.EncodeToString(make([]byte, 32))
}

func TestEncryptDecryptAccountNumberRoundTrip(t *testing.T) {
	t.Setenv("PAYOUT_ENCRYPTION_KEY", key32())

	const plain = "123456789012"

	first, err := secure.EncryptAccountNumber(plain)
	require.NoError(t, err)
	second, err := secure.EncryptAccountNumber(plain)
	require.NoError(t, err)
	assert.NotEqual(t, first, second, "random nonce must yield distinct ciphertexts")
	assert.NotContains(t, first, plain)

	got, err := secure.DecryptAccountNumber(first)
	require.NoError(t, err)
	assert.Equal(t, plain, got)

	// Tampered ciphertext (last base64 char flipped) must not decrypt.
	tampered := first[:len(first)-1] + strings.ToUpper(string(first[len(first)-1]))
	if tampered == first {
		tampered = first[:len(first)-1] + "A"
	}
	_, err = secure.DecryptAccountNumber(tampered)
	assert.Error(t, err)
}

func TestEncryptAccountNumberKeyRequired(t *testing.T) {
	t.Setenv("PAYOUT_ENCRYPTION_KEY", "")

	_, err := secure.EncryptAccountNumber("123456789012")
	assert.ErrorIs(t, err, secure.ErrPayoutKeyNotConfigured)

	_, err = secure.DecryptAccountNumber("anything")
	assert.ErrorIs(t, err, secure.ErrPayoutKeyNotConfigured)
}

func TestEncryptAccountNumberRejectsWrongKeyLength(t *testing.T) {
	for name, key := range map[string]string{
		"not base64": "not-base64!!",
		"16 bytes":   base64.StdEncoding.EncodeToString(make([]byte, 16)),
		"31 bytes":   base64.StdEncoding.EncodeToString(make([]byte, 31)),
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("PAYOUT_ENCRYPTION_KEY", key)
			_, err := secure.EncryptAccountNumber("123456789012")
			assert.ErrorIs(t, err, secure.ErrPayoutKeyNotConfigured)
		})
	}
}
