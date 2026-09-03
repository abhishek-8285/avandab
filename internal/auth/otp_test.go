package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOTPStore_CreateAndVerifyHappyPath(t *testing.T) {
	s := NewOTPStore(0)
	code, err := s.Create("+919876543210")
	require.NoError(t, err)
	require.Len(t, code, 6)

	require.True(t, s.Verify("+919876543210", code), "correct code must verify")
	require.False(t, s.Verify("+919876543210", code), "code is single-use")
}

func TestOTPStore_WrongCodeFails(t *testing.T) {
	s := NewOTPStore(0)
	code, err := s.Create("+919876543210")
	require.NoError(t, err)

	require.False(t, s.Verify("+919876543210", "000000"))
	require.False(t, s.Verify("+919876543210", "999999"))
	require.False(t, s.Verify("", code), "empty phone never verifies")

	// The real code still works while within the attempt budget.
	require.True(t, s.Verify("+919876543210", code))
}

func TestOTPStore_AttemptsExhaustedInvalidates(t *testing.T) {
	s := NewOTPStore(time.Hour)
	code, err := s.Create("+919876543210")
	require.NoError(t, err)

	for i := 0; i < OTPMaxAttempts; i++ {
		require.False(t, s.Verify("+919876543210", "000001"))
	}
	// After the budget is spent even the correct code fails.
	require.False(t, s.Verify("+919876543210", code))
}

func TestOTPStore_Expiry(t *testing.T) {
	s := NewOTPStore(200 * time.Millisecond)
	code, err := s.Create("+919876543210")
	require.NoError(t, err)

	time.Sleep(300 * time.Millisecond)
	require.False(t, s.Verify("+919876543210", code), "expired code must fail")
}

func TestOTPStore_ResendThrottled(t *testing.T) {
	s := NewOTPStore(0)
	_, err := s.Create("+919876543210")
	require.NoError(t, err)

	_, err = s.Create("+919876543210")
	require.Error(t, err)
	require.Contains(t, err.Error(), "resend blocked")

	require.Greater(t, s.ResendBlockedFor("+919876543210"), time.Duration(0))
	require.Equal(t, time.Duration(0), s.ResendBlockedFor("+919999999"),
		"never-sent phone is not blocked")
}

func TestOTPStore_RequiresPhone(t *testing.T) {
	s := NewOTPStore(0)
	_, err := s.Create("")
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "phone required"))
	require.False(t, s.Verify("", "123456"))
}
