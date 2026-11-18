package auth

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// A one-time code must be redeemable exactly once. With a shared cache the
// verify path reads, edits and re-writes the record; without an atomic claim
// two simultaneous requests can both read the same unconsumed code and both
// succeed (replay). Four "replicas" sharing one cache model a multi-instance
// deployment, where a local mutex alone cannot stop the race.
func TestOTPStore_CachedConcurrentVerifyIsSingleUse(t *testing.T) {
	cache := newSharedTestCache(t)
	phone := "+919876543210"
	code, err := NewOTPStoreWithCache(0, cache).Create(phone)
	require.NoError(t, err)
	require.Len(t, code, 6)

	const (
		replicas = 4
		callers  = 32
	)
	stores := make([]*OTPStore, replicas)
	for i := range stores {
		stores[i] = NewOTPStoreWithCache(0, cache)
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	var accepted int64
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			if stores[i%replicas].Verify(phone, code) {
				atomic.AddInt64(&accepted, 1)
			}
		}(i)
	}
	close(start)
	wg.Wait()

	require.Equal(t, int64(1), accepted,
		"a one-time OTP must be accepted by exactly one of many concurrent verifications")
}
