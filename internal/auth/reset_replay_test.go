package auth

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// A password-reset link is single-use. Two replicas sharing a cache must not
// both redeem the same token when requests arrive together.
func TestResetTokenStore_CachedConcurrentConsumeIsSingleUse(t *testing.T) {
	cache := newSharedTestCache(t)
	email := "replay@example.com"
	token, err := NewResetTokenStoreWithCache(0, cache).Create(email)
	require.NoError(t, err)

	const (
		replicas = 4
		callers  = 32
	)
	stores := make([]*ResetTokenStore, replicas)
	for i := range stores {
		stores[i] = NewResetTokenStoreWithCache(0, cache)
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	var accepted int64
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			if got, ok := stores[i%replicas].Consume(token); ok && got == email {
				atomic.AddInt64(&accepted, 1)
			}
		}(i)
	}
	close(start)
	wg.Wait()

	require.Equal(t, int64(1), accepted,
		"a one-time reset token must be redeemed by exactly one of many concurrent requests")
}
