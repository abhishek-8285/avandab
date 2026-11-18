package auth

import (
	"context"
	"testing"
	"time"

	"transport-app/internal/cache"

	"github.com/stretchr/testify/require"
)

// sharedCacheSettings selects the real in-process cache backend. Two stores
// handed this same instance model two replicas sharing one backing store.
type sharedCacheSettings struct{}

func (sharedCacheSettings) GetDriver() string            { return cache.DriverMemory }
func (sharedCacheSettings) GetRedisAddr() string         { return "" }
func (sharedCacheSettings) GetRedisPassword() string     { return "" }
func (sharedCacheSettings) GetRedisDB() int              { return 0 }
func (sharedCacheSettings) GetDefaultTTL() time.Duration { return time.Minute }
func (sharedCacheSettings) GetKeyPrefix() string         { return "auth-test:" }

func newSharedTestCache(t *testing.T) cache.Cache {
	t.Helper()
	c, err := cache.New(context.Background(), sharedCacheSettings{}, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		if closer, ok := c.(cache.Closer); ok {
			_ = closer.Close()
		}
	})
	return c
}

func TestOTPStore_CrossInstanceSharedCache(t *testing.T) {
	c := newSharedTestCache(t)
	a := NewOTPStoreWithCache(0, c)
	b := NewOTPStoreWithCache(0, c)

	// A code minted by one replica verifies in the other.
	phone := "+919876543219"
	code, err := a.Create(phone)
	require.NoError(t, err)
	require.Len(t, code, 6)
	require.True(t, b.Verify(phone, code), "code minted by one replica must verify in another sharing the cache")
	require.False(t, a.Verify(phone, code), "a consumed code must stay single-use across replicas")

	// The resend throttle is shared state too, not per-process.
	phone2 := "+919876543220"
	_, err = a.Create(phone2)
	require.NoError(t, err)
	_, err = b.Create(phone2)
	require.Error(t, err, "a send from one replica must throttle the next on another")
	require.Contains(t, err.Error(), "resend blocked")
}

func TestResetTokenStore_CrossInstanceSharedCache(t *testing.T) {
	c := newSharedTestCache(t)
	a := NewResetTokenStoreWithCache(0, c)
	b := NewResetTokenStoreWithCache(0, c)

	email := "shared@example.com"
	token, err := a.Create(email)
	require.NoError(t, err)
	got, ok := b.Consume(token)
	require.True(t, ok, "token minted by one replica must be redeemable by another")
	require.Equal(t, email, got)
	_, ok = a.Consume(token)
	require.False(t, ok, "a redeemed token must stay single-use across replicas")
}

// A 24h email-verification token must never redeem as a password reset. Both
// stores share one cache, so the key namespace has to differ per purpose.
func TestResetTokenStore_VerificationTokenCannotRedeemAsReset(t *testing.T) {
	c := newSharedTestCache(t)
	resets := NewResetTokenStoreWithCache(0, c)
	verifies := NewResetTokenStoreWithCache(24*time.Hour, c).WithNamespace("auth:verify:")

	token, err := verifies.Create("victim@example.com")
	require.NoError(t, err)

	_, ok := resets.Consume(token)
	require.False(t, ok, "a verify-email token must not redeem as a password reset")
	got, ok := verifies.Consume(token)
	require.True(t, ok, "the verification token must still redeem in its own store")
	require.Equal(t, "victim@example.com", got)
}
