package auth

import (
	"context"
	"errors"
	"testing"

	"transport-app/internal/cache"

	"github.com/stretchr/testify/require"
)

// deleteFailCache keeps Get/Set working but makes every purge fail, modeling
// a Redis blip at the exact moment a one-time token is consumed.
type deleteFailCache struct {
	cache.Cache
}

func (deleteFailCache) Delete(context.Context, string) error {
	return errors.New("forced cache delete failure")
}

func TestOTPStore_VerifyFailsClosedWhenPurgeFails(t *testing.T) {
	s := NewOTPStoreWithCache(0, deleteFailCache{newSharedTestCache(t)})
	phone := "+919876543211"
	code, err := s.Create(phone)
	require.NoError(t, err)

	// If the code cannot be purged it must not be accepted — otherwise the
	// same code stays replayable for its whole TTL.
	require.False(t, s.Verify(phone, code))
	require.False(t, s.Verify(phone, code))
}

func TestResetTokenStore_ConsumeFailsClosedWhenPurgeFails(t *testing.T) {
	s := NewResetTokenStoreWithCache(0, deleteFailCache{newSharedTestCache(t)})
	token, err := s.Create("purge-fail@example.com")
	require.NoError(t, err)

	email, ok := s.Consume(token)
	require.False(t, ok, "a token that cannot be purged must not be redeemed")
	require.Empty(t, email)
}
