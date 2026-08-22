package cache_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"

	"transport-app/internal/cache"
)

type testSettings struct {
	driver   string
	addr     string
	password string
	db       int
	ttl      time.Duration
	prefix   string
}

func (t *testSettings) GetDriver() string            { return t.driver }
func (t *testSettings) GetRedisAddr() string         { return t.addr }
func (t *testSettings) GetRedisPassword() string     { return t.password }
func (t *testSettings) GetRedisDB() int              { return t.db }
func (t *testSettings) GetDefaultTTL() time.Duration { return t.ttl }
func (t *testSettings) GetKeyPrefix() string         { return t.prefix }

// newMemoryCache is a helper that creates a memory cache with given TTL.
func newMemoryCache(t *testing.T, ttl time.Duration) cache.Cache {
	t.Helper()
	c, err := cache.New(context.Background(), &testSettings{driver: "memory", ttl: ttl}, nil)
	require.NoError(t, err)
	require.NotNil(t, c)
	t.Cleanup(func() {
		if closer, ok := c.(cache.Closer); ok {
			_ = closer.Close()
		}
	})
	return c
}

func startMiniredis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	m, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(func() { m.Close() })
	return m
}

// ---------------------------------------------------------------------------
// Noop backend — lines 110,111,114 (previously 0% coverage)
// ---------------------------------------------------------------------------

func TestNoop_Direct_Get_Miss(t *testing.T) {
	var n cache.Noop
	ctx := context.Background()

	val, ok, err := n.Get(ctx, "any")
	require.NoError(t, err)
	require.False(t, ok)
	require.Nil(t, val)
}

func TestNoop_Direct_Set_NoError(t *testing.T) {
	var n cache.Noop
	require.NoError(t, n.Set(context.Background(), "k", []byte("v"), time.Minute))
	require.NoError(t, n.Set(context.Background(), "k", []byte("v"), 0))
	require.NoError(t, n.Set(context.Background(), "k", nil, time.Second))
}

func TestNoop_Direct_Delete_NoError(t *testing.T) {
	var n cache.Noop
	ctx := context.Background()
	require.NoError(t, n.Delete(ctx, "k"))
	require.NoError(t, n.Delete(ctx, "missing"))
	require.NoError(t, n.Delete(ctx, ""))
}

func TestNoop_ViaNew_GetSetDelete(t *testing.T) {
	ctx := context.Background()
	c, err := cache.New(ctx, &testSettings{driver: "none"}, nil)
	require.NoError(t, err)
	_, ok := c.(cache.Noop)
	require.True(t, ok, "expected cache.Noop")

	val, ok, err := c.Get(ctx, "k")
	require.NoError(t, err)
	require.False(t, ok)
	require.Nil(t, val)

	require.NoError(t, c.Set(ctx, "k", []byte("v"), time.Minute))

	val, ok, err = c.Get(ctx, "k")
	require.NoError(t, err)
	require.False(t, ok, "Noop must always miss")
	require.Nil(t, val)

	require.NoError(t, c.Delete(ctx, "k"))
	require.NoError(t, c.Delete(ctx, "missing"))
}

func TestNoop_DoesNotImplementIncrementer(t *testing.T) {
	var n cache.Noop
	_, ok := interface{}(n).(cache.Incrementer)
	require.False(t, ok, "Noop must NOT implement Incrementer so callers fallback to local limit")
}

func TestNoop_DoesNotImplementCloser(t *testing.T) {
	var n cache.Noop
	_, ok := interface{}(n).(cache.Closer)
	require.False(t, ok)
}

// ---------------------------------------------------------------------------
// New / MustNew routing
// ---------------------------------------------------------------------------

func TestNew_None(t *testing.T) {
	c, err := cache.New(context.Background(), &testSettings{driver: "none"}, nil)
	require.NoError(t, err)
	_, ok := c.(cache.Noop)
	require.True(t, ok)
}

func TestNew_EmptyDriverDefaultsToNoop(t *testing.T) {
	c, err := cache.New(context.Background(), &testSettings{}, nil)
	require.NoError(t, err)
	_, ok := c.(cache.Noop)
	require.True(t, ok)
}

func TestNew_DriverCaseInsensitive(t *testing.T) {
	tests := []struct {
		driver string
		want   string
	}{
		{"NONE", "noop"},
		{"None", "noop"},
		{"MEMORY", "memory"},
		{"Memory", "memory"},
		{"memory", "memory"},
		{"", "noop"},
	}
	for _, tc := range tests {
		t.Run(tc.driver, func(t *testing.T) {
			c, err := cache.New(context.Background(), &testSettings{driver: tc.driver, ttl: time.Minute}, nil)
			require.NoError(t, err)
			if tc.want == "noop" {
				_, ok := c.(cache.Noop)
				require.True(t, ok, "want Noop for %q", tc.driver)
			} else {
				_, ok := c.(cache.Closer)
				require.True(t, ok, "want memory Closer for %q", tc.driver)
				_ = c.(cache.Closer).Close()
			}
		})
	}
}

func TestNew_Memory(t *testing.T) {
	s := &testSettings{driver: "memory", ttl: time.Hour, prefix: "mvtms:"}
	c, err := cache.New(context.Background(), s, slog.Default())
	require.NoError(t, err)
	defer func() { _ = c.(cache.Closer).Close() }()

	ctx := context.Background()

	_, ok, err := c.Get(ctx, "k")
	require.NoError(t, err)
	require.False(t, ok)

	require.NoError(t, c.Set(ctx, "k", []byte("v1"), 0))
	got, ok, err := c.Get(ctx, "k")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "v1", string(got))

	// Mutating the returned bytes must not corrupt the cached value.
	got[0] = 'X'
	got2, _, _ := c.Get(ctx, "k")
	require.Equal(t, "v1", string(got2))

	require.NoError(t, c.Delete(ctx, "k"))
	_, ok, _ = c.Get(ctx, "k")
	require.False(t, ok)

	// Deleting a missing key is not an error.
	require.NoError(t, c.Delete(ctx, "missing"))
}

func TestNew_MemoryExpiry(t *testing.T) {
	c, err := cache.New(context.Background(), &testSettings{
		driver: "memory",
		ttl:    30 * time.Millisecond,
	}, nil)
	require.NoError(t, err)
	defer func() { _ = c.(cache.Closer).Close() }()

	ctx := context.Background()
	require.NoError(t, c.Set(ctx, "k", []byte("v"), 0))
	time.Sleep(60 * time.Millisecond)
	_, ok, _ := c.Get(ctx, "k")
	require.False(t, ok, "Get after TTL should be miss")
}

func TestNew_UnsupportedDriver(t *testing.T) {
	_, err := cache.New(context.Background(), &testSettings{driver: "memcached"}, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported driver")
}

func TestNew_RedisUnreachable(t *testing.T) {
	s := &testSettings{driver: "redis", addr: "127.0.0.1:1"}
	_, err := cache.New(context.Background(), s, nil)
	require.Error(t, err)
}

// TestNew_RedisLive runs only when REDIS_TEST_ADDR points at a real server:
// go test ./internal/cache/ -run RedisLive
func TestNew_RedisLive(t *testing.T) {
	addr := os.Getenv("REDIS_TEST_ADDR")
	if addr == "" {
		t.Skip("set REDIS_TEST_ADDR=host:port to run live redis tests")
	}
	s := &testSettings{driver: "redis", addr: addr, ttl: time.Minute, prefix: "test:"}
	c, err := cache.New(context.Background(), s, nil)
	require.NoError(t, err)
	defer func() { _ = c.(cache.Closer).Close() }()

	ctx := context.Background()
	require.NoError(t, c.Set(ctx, "live", []byte("ok"), time.Minute))
	got, ok, err := c.Get(ctx, "live")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "ok", string(got))
	require.NoError(t, c.Delete(ctx, "live"))
}

func TestMustNew_FallsBackToNoopOnError(t *testing.T) {
	c := cache.MustNew(context.Background(), &testSettings{driver: "redis", addr: "127.0.0.1:1"}, nil)
	_, ok := c.(cache.Noop)
	require.True(t, ok)
}

func TestMustNew_FallsBackWithLogger(t *testing.T) {
	logger := slog.Default()
	c := cache.MustNew(context.Background(), &testSettings{driver: "redis", addr: "127.0.0.1:1"}, logger)
	_, ok := c.(cache.Noop)
	require.True(t, ok)
}

func TestMustNew_SuccessReturnsRealBackend(t *testing.T) {
	c := cache.MustNew(context.Background(), &testSettings{driver: "memory", ttl: time.Minute}, nil)
	_, ok := c.(cache.Closer)
	require.True(t, ok)
	_ = c.(cache.Closer).Close()

	c2 := cache.MustNew(context.Background(), &testSettings{driver: "none"}, nil)
	_, ok = c2.(cache.Noop)
	require.True(t, ok)
}

// ---------------------------------------------------------------------------
// Memory: Get / Set / Delete in isolation
// ---------------------------------------------------------------------------

func TestMemory_Get_Miss_NotFound(t *testing.T) {
	c := newMemoryCache(t, time.Minute)
	_, ok, err := c.Get(context.Background(), "absent")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestMemory_Get_Hit(t *testing.T) {
	c := newMemoryCache(t, time.Minute)
	ctx := context.Background()
	require.NoError(t, c.Set(ctx, "k", []byte("hello"), time.Minute))
	val, ok, err := c.Get(ctx, "k")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("hello"), val)
}

func TestMemory_Get_Expired_ReturnsMiss(t *testing.T) {
	c := newMemoryCache(t, 20*time.Millisecond)
	ctx := context.Background()
	require.NoError(t, c.Set(ctx, "k", []byte("v"), 20*time.Millisecond))
	// before expiry still hit
	_, ok, _ := c.Get(ctx, "k")
	require.True(t, ok)
	time.Sleep(40 * time.Millisecond)
	_, ok, err := c.Get(ctx, "k")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestMemory_Set_ExplicitTTL_OverridesDefault(t *testing.T) {
	c := newMemoryCache(t, time.Hour)
	ctx := context.Background()
	// default is 1 hour, but explicit 30ms should expire quickly
	require.NoError(t, c.Set(ctx, "k", []byte("v"), 30*time.Millisecond))
	time.Sleep(60 * time.Millisecond)
	_, ok, _ := c.Get(ctx, "k")
	require.False(t, ok)
}

func TestMemory_Set_UsesDefaultTTL_WhenZero(t *testing.T) {
	c := newMemoryCache(t, 40*time.Millisecond)
	ctx := context.Background()
	require.NoError(t, c.Set(ctx, "k", []byte("v"), 0)) // 0 -> default 40ms
	time.Sleep(80 * time.Millisecond)
	_, ok, _ := c.Get(ctx, "k")
	require.False(t, ok, "ttl 0 should use defaultTTL and expire")
}

func TestMemory_Set_UsesDefaultTTL_WhenNegative(t *testing.T) {
	c := newMemoryCache(t, 40*time.Millisecond)
	ctx := context.Background()
	require.NoError(t, c.Set(ctx, "k", []byte("v"), -time.Second))
	time.Sleep(80 * time.Millisecond)
	_, ok, _ := c.Get(ctx, "k")
	require.False(t, ok)
}

func TestMemory_Set_UsesDefaultTTL_WhenZero_DoesNotExpireImmediately(t *testing.T) {
	// Verify that zero TTL with 5m default doesn't expire in 50ms
	c, err := cache.New(context.Background(), &testSettings{driver: "memory", ttl: 0}, nil)
	require.NoError(t, err)
	defer func() { _ = c.(cache.Closer).Close() }()
	ctx := context.Background()
	require.NoError(t, c.Set(ctx, "k", []byte("v"), 0))
	time.Sleep(50 * time.Millisecond)
	_, ok, _ := c.Get(ctx, "k")
	require.True(t, ok, "default 5m TTL should not expire in 50ms")
}

func TestMemory_Set_CopiesInputBytes(t *testing.T) {
	c := newMemoryCache(t, time.Minute)
	ctx := context.Background()
	orig := []byte("original")
	require.NoError(t, c.Set(ctx, "k", orig, time.Minute))
	orig[0] = 'X'
	orig[1] = 'Y'
	val, ok, _ := c.Get(ctx, "k")
	require.True(t, ok)
	require.Equal(t, "original", string(val))
}

func TestMemory_Get_CopiesOutputBytes(t *testing.T) {
	c := newMemoryCache(t, time.Minute)
	ctx := context.Background()
	require.NoError(t, c.Set(ctx, "k", []byte("secret"), time.Minute))
	val, ok, _ := c.Get(ctx, "k")
	require.True(t, ok)
	val[0] = 'X'
	val2, ok, _ := c.Get(ctx, "k")
	require.True(t, ok)
	require.Equal(t, "secret", string(val2))
}

func TestMemory_Set_Overwrite(t *testing.T) {
	c := newMemoryCache(t, time.Minute)
	ctx := context.Background()
	require.NoError(t, c.Set(ctx, "k", []byte("v1"), time.Minute))
	require.NoError(t, c.Set(ctx, "k", []byte("v2"), time.Minute))
	val, ok, _ := c.Get(ctx, "k")
	require.True(t, ok)
	require.Equal(t, "v2", string(val))
}

func TestMemory_Set_EmptyValue(t *testing.T) {
	c := newMemoryCache(t, time.Minute)
	ctx := context.Background()
	require.NoError(t, c.Set(ctx, "k", []byte{}, time.Minute))
	val, ok, err := c.Get(ctx, "k")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 0, len(val))
}

func TestMemory_Set_NilValue(t *testing.T) {
	c := newMemoryCache(t, time.Minute)
	ctx := context.Background()
	require.NoError(t, c.Set(ctx, "k", nil, time.Minute))
	_, ok, _ := c.Get(ctx, "k")
	require.True(t, ok)
}

func TestMemory_Delete_Hit(t *testing.T) {
	c := newMemoryCache(t, time.Minute)
	ctx := context.Background()
	require.NoError(t, c.Set(ctx, "k", []byte("v"), time.Minute))
	require.NoError(t, c.Delete(ctx, "k"))
	_, ok, _ := c.Get(ctx, "k")
	require.False(t, ok)
}

func TestMemory_Delete_Miss_NoError(t *testing.T) {
	c := newMemoryCache(t, time.Minute)
	require.NoError(t, c.Delete(context.Background(), "absent"))
	require.NoError(t, c.Delete(context.Background(), "absent"))
}

func TestMemory_Delete_AfterExpired(t *testing.T) {
	c := newMemoryCache(t, 20*time.Millisecond)
	ctx := context.Background()
	require.NoError(t, c.Set(ctx, "k", []byte("v"), 20*time.Millisecond))
	time.Sleep(40 * time.Millisecond)
	// Delete on already-expired key should still be no error (Delete just removes map entry)
	require.NoError(t, c.Delete(ctx, "k"))
	_, ok, _ := c.Get(ctx, "k")
	require.False(t, ok)
}

func TestMemory_Delete_IsolatedKeys(t *testing.T) {
	c := newMemoryCache(t, time.Minute)
	ctx := context.Background()
	require.NoError(t, c.Set(ctx, "k1", []byte("v1"), time.Minute))
	require.NoError(t, c.Set(ctx, "k2", []byte("v2"), time.Minute))
	require.NoError(t, c.Delete(ctx, "k1"))
	_, ok, _ := c.Get(ctx, "k1")
	require.False(t, ok)
	val, ok, _ := c.Get(ctx, "k2")
	require.True(t, ok)
	require.Equal(t, "v2", string(val))
}

func TestMemory_Close_ClearsEntries(t *testing.T) {
	c := newMemoryCache(t, time.Minute)
	ctx := context.Background()
	require.NoError(t, c.Set(ctx, "k", []byte("v"), time.Minute))
	_, ok, _ := c.Get(ctx, "k")
	require.True(t, ok)

	require.NoError(t, c.(cache.Closer).Close())
	_, ok, _ = c.Get(ctx, "k")
	require.False(t, ok, "Close should clear all entries")
}

func TestMemory_Close_Idempotent(t *testing.T) {
	cRaw, err := cache.New(context.Background(), &testSettings{driver: "memory", ttl: time.Minute}, nil)
	require.NoError(t, err)
	closer := cRaw.(cache.Closer)
	require.NoError(t, closer.Close())
	require.NoError(t, closer.Close())
	require.NoError(t, closer.Close())
}

func TestMemory_Close_CanStillSetAfter(t *testing.T) {
	// Close recreates map as empty; Set should still work afterwards (no panic)
	cRaw, err := cache.New(context.Background(), &testSettings{driver: "memory", ttl: time.Minute}, nil)
	require.NoError(t, err)
	closer := cRaw.(cache.Closer)
	require.NoError(t, closer.Close())
	require.NoError(t, cRaw.Set(context.Background(), "k", []byte("v"), time.Minute))
	val, ok, _ := cRaw.Get(context.Background(), "k")
	require.True(t, ok)
	require.Equal(t, "v", string(val))
}

func TestMemory_IsIncrementer(t *testing.T) {
	c := newMemoryCache(t, time.Minute)
	_, ok := c.(cache.Incrementer)
	require.True(t, ok)
}

func TestMemory_NewMemoryCache_DefaultTTL_Fallback(t *testing.T) {
	// ttl <=0 should default to 5 minutes; verify via behavior
	for _, ttl := range []time.Duration{0, -time.Minute, -time.Second} {
		c, err := cache.New(context.Background(), &testSettings{driver: "memory", ttl: ttl}, nil)
		require.NoError(t, err)
		closer := c.(cache.Closer)
		require.NoError(t, closer.Close())
		// No panic, and Set with 0 uses 5m default (not expired quickly)
		ctx := context.Background()
		// Re-create to test Set behavior with default
		c2, _ := cache.New(context.Background(), &testSettings{driver: "memory", ttl: ttl}, nil)
		require.NoError(t, c2.Set(ctx, "k", []byte("v"), 0))
		_, ok, _ := c2.Get(ctx, "k")
		require.True(t, ok)
		_ = c2.(cache.Closer).Close()
	}
}

// ---------------------------------------------------------------------------
// Memory: Increment (fixed-window counter) — eviction/TTL logic
// ---------------------------------------------------------------------------

func TestMemory_Increment_FirstHit(t *testing.T) {
	c := newMemoryCache(t, time.Minute)
	inc := c.(cache.Incrementer)
	n, err := inc.Increment(context.Background(), "cnt", time.Minute)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
}

func TestMemory_Increment_SecondHit_Increments(t *testing.T) {
	c := newMemoryCache(t, time.Minute)
	inc := c.(cache.Incrementer)
	ctx := context.Background()
	n1, _ := inc.Increment(ctx, "cnt", time.Minute)
	n2, _ := inc.Increment(ctx, "cnt", time.Minute)
	n3, _ := inc.Increment(ctx, "cnt", time.Minute)
	require.Equal(t, int64(1), n1)
	require.Equal(t, int64(2), n2)
	require.Equal(t, int64(3), n3)
}

func TestMemory_Increment_DifferentKeys_Isolated(t *testing.T) {
	c := newMemoryCache(t, time.Minute)
	inc := c.(cache.Incrementer)
	ctx := context.Background()
	a1, _ := inc.Increment(ctx, "a", time.Minute)
	b1, _ := inc.Increment(ctx, "b", time.Minute)
	a2, _ := inc.Increment(ctx, "a", time.Minute)
	require.Equal(t, int64(1), a1)
	require.Equal(t, int64(1), b1)
	require.Equal(t, int64(2), a2)
}

func TestMemory_Increment_UsesDefaultTTL_WhenZero(t *testing.T) {
	c := newMemoryCache(t, 40*time.Millisecond)
	inc := c.(cache.Incrementer)
	ctx := context.Background()
	n, err := inc.Increment(ctx, "cnt", 0) // 0 -> default 40ms
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
	time.Sleep(60 * time.Millisecond)
	n2, err := inc.Increment(ctx, "cnt", 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), n2, "window should have expired and reset")
}

func TestMemory_Increment_UsesDefaultTTL_WhenNegative(t *testing.T) {
	c := newMemoryCache(t, 40*time.Millisecond)
	inc := c.(cache.Incrementer)
	ctx := context.Background()
	n, _ := inc.Increment(ctx, "cnt", -time.Second)
	require.Equal(t, int64(1), n)
	time.Sleep(60 * time.Millisecond)
	n2, _ := inc.Increment(ctx, "cnt", -time.Second)
	require.Equal(t, int64(1), n2)
}

func TestMemory_Increment_ExpiryResetsWindow(t *testing.T) {
	c := newMemoryCache(t, time.Minute)
	inc := c.(cache.Incrementer)
	ctx := context.Background()
	ttl := 40 * time.Millisecond
	n1, _ := inc.Increment(ctx, "cnt", ttl)
	require.Equal(t, int64(1), n1)
	n2, _ := inc.Increment(ctx, "cnt", ttl)
	require.Equal(t, int64(2), n2)
	time.Sleep(60 * time.Millisecond)
	n3, _ := inc.Increment(ctx, "cnt", ttl)
	require.Equal(t, int64(1), n3, "after window expiry counter should reset to 1")
	n4, _ := inc.Increment(ctx, "cnt", ttl)
	require.Equal(t, int64(2), n4)
}

func TestMemory_Increment_Expiry_IsolatedPerKey(t *testing.T) {
	c := newMemoryCache(t, time.Minute)
	inc := c.(cache.Incrementer)
	ctx := context.Background()
	ttlA := 30 * time.Millisecond
	ttlB := time.Minute
	_, _ = inc.Increment(ctx, "a", ttlA)
	_, _ = inc.Increment(ctx, "b", ttlB)
	_, _ = inc.Increment(ctx, "a", ttlA) // a=2
	time.Sleep(50 * time.Millisecond)
	nA, _ := inc.Increment(ctx, "a", ttlA)
	require.Equal(t, int64(1), nA, "a should have expired")
	nB, _ := inc.Increment(ctx, "b", ttlB)
	require.Equal(t, int64(2), nB, "b should not have expired")
}

// ---------------------------------------------------------------------------
// Memory: eviction / TTL edge cases
// ---------------------------------------------------------------------------

func TestMemory_Eviction_MultipleKeys_ExpiryOnlyExpired(t *testing.T) {
	c := newMemoryCache(t, time.Minute)
	ctx := context.Background()
	require.NoError(t, c.Set(ctx, "short", []byte("v"), 30*time.Millisecond))
	require.NoError(t, c.Set(ctx, "long", []byte("v"), time.Minute))
	time.Sleep(50 * time.Millisecond)
	_, okShort, _ := c.Get(ctx, "short")
	require.False(t, okShort)
	_, okLong, _ := c.Get(ctx, "long")
	require.True(t, okLong)
}

func TestMemory_Set_NilValue_ThenOverwrite(t *testing.T) {
	c := newMemoryCache(t, time.Minute)
	ctx := context.Background()
	require.NoError(t, c.Set(ctx, "k", nil, time.Minute))
	_, ok, _ := c.Get(ctx, "k")
	require.True(t, ok)
	require.NoError(t, c.Set(ctx, "k", []byte("new"), time.Minute))
	val, ok, _ := c.Get(ctx, "k")
	require.True(t, ok)
	require.Equal(t, "new", string(val))
}

// ---------------------------------------------------------------------------
// Redis via miniredis — covers redisCache Get/Set/Delete/Increment/fullKey/Close
// Isolated, no external DB needed.
// ---------------------------------------------------------------------------

func TestRedis_Miniredis_GetSetDelete(t *testing.T) {
	m := startMiniredis(t)
	s := &testSettings{driver: "redis", addr: m.Addr(), ttl: time.Minute, prefix: "test:"}
	c, err := cache.New(context.Background(), s, nil)
	require.NoError(t, err)
	defer func() { _ = c.(cache.Closer).Close() }()

	ctx := context.Background()
	_, ok, err := c.Get(ctx, "absent")
	require.NoError(t, err)
	require.False(t, ok)

	require.NoError(t, c.Set(ctx, "k", []byte("hello"), time.Minute))
	got, ok, err := c.Get(ctx, "k")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "hello", string(got))

	// Overwrite
	require.NoError(t, c.Set(ctx, "k", []byte("world"), time.Minute))
	got, ok, _ = c.Get(ctx, "k")
	require.True(t, ok)
	require.Equal(t, "world", string(got))

	require.NoError(t, c.Delete(ctx, "k"))
	_, ok, _ = c.Get(ctx, "k")
	require.False(t, ok)

	// Delete missing is not error
	require.NoError(t, c.Delete(ctx, "missing"))
}

func TestRedis_Miniredis_TTLExpiry(t *testing.T) {
	m := startMiniredis(t)
	s := &testSettings{driver: "redis", addr: m.Addr(), ttl: time.Minute, prefix: "t:"}
	c, err := cache.New(context.Background(), s, nil)
	require.NoError(t, err)
	defer func() { _ = c.(cache.Closer).Close() }()

	ctx := context.Background()
	require.NoError(t, c.Set(ctx, "k", []byte("v"), time.Second))
	// Fast-forward miniredis clock instead of real sleep
	m.FastForward(2 * time.Second)
	_, ok, err := c.Get(ctx, "k")
	require.NoError(t, err)
	require.False(t, ok, "should be expired after TTL")
}

func TestRedis_Miniredis_TTL_UsesDefaultWhenZero(t *testing.T) {
	m := startMiniredis(t)
	s := &testSettings{driver: "redis", addr: m.Addr(), ttl: 1 * time.Second, prefix: "t:"}
	c, err := cache.New(context.Background(), s, nil)
	require.NoError(t, err)
	defer func() { _ = c.(cache.Closer).Close() }()

	ctx := context.Background()
	require.NoError(t, c.Set(ctx, "k", []byte("v"), 0)) // 0 -> default 1s
	m.FastForward(2 * time.Second)
	_, ok, _ := c.Get(ctx, "k")
	require.False(t, ok)
}

func TestRedis_Miniredis_TTL_NegativeUsesDefault(t *testing.T) {
	m := startMiniredis(t)
	s := &testSettings{driver: "redis", addr: m.Addr(), ttl: 1 * time.Second, prefix: "t:"}
	c, err := cache.New(context.Background(), s, nil)
	require.NoError(t, err)
	defer func() { _ = c.(cache.Closer).Close() }()

	ctx := context.Background()
	require.NoError(t, c.Set(ctx, "k", []byte("v"), -time.Second))
	m.FastForward(2 * time.Second)
	_, ok, _ := c.Get(ctx, "k")
	require.False(t, ok)
}

func TestRedis_Miniredis_PrefixIsolation(t *testing.T) {
	m := startMiniredis(t)
	ctx := context.Background()

	s1 := &testSettings{driver: "redis", addr: m.Addr(), ttl: time.Minute, prefix: "env1:"}
	c1, err := cache.New(ctx, s1, nil)
	require.NoError(t, err)
	defer func() { _ = c1.(cache.Closer).Close() }()

	s2 := &testSettings{driver: "redis", addr: m.Addr(), ttl: time.Minute, prefix: "env2:"}
	c2, err := cache.New(ctx, s2, nil)
	require.NoError(t, err)
	defer func() { _ = c2.(cache.Closer).Close() }()

	require.NoError(t, c1.Set(ctx, "k", []byte("v1"), time.Minute))
	require.NoError(t, c2.Set(ctx, "k", []byte("v2"), time.Minute))

	got1, ok, _ := c1.Get(ctx, "k")
	require.True(t, ok)
	require.Equal(t, "v1", string(got1))

	got2, ok, _ := c2.Get(ctx, "k")
	require.True(t, ok)
	require.Equal(t, "v2", string(got2))

	// Underlying redis should have two distinct keys
	require.True(t, m.Exists("env1:k"))
	require.True(t, m.Exists("env2:k"))
}

func TestRedis_Miniredis_Increment_Basic(t *testing.T) {
	m := startMiniredis(t)
	s := &testSettings{driver: "redis", addr: m.Addr(), ttl: time.Minute, prefix: "t:"}
	c, err := cache.New(context.Background(), s, nil)
	require.NoError(t, err)
	defer func() { _ = c.(cache.Closer).Close() }()

	inc := c.(cache.Incrementer)
	ctx := context.Background()
	n, err := inc.Increment(ctx, "cnt", time.Minute)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
	n, err = inc.Increment(ctx, "cnt", time.Minute)
	require.NoError(t, err)
	require.Equal(t, int64(2), n)
	n, err = inc.Increment(ctx, "cnt", time.Minute)
	require.NoError(t, err)
	require.Equal(t, int64(3), n)
}

func TestRedis_Miniredis_Increment_TTLWindow(t *testing.T) {
	m := startMiniredis(t)
	s := &testSettings{driver: "redis", addr: m.Addr(), ttl: time.Minute, prefix: "t:"}
	c, err := cache.New(context.Background(), s, nil)
	require.NoError(t, err)
	defer func() { _ = c.(cache.Closer).Close() }()

	inc := c.(cache.Incrementer)
	ctx := context.Background()
	ttl := 1 * time.Second
	n1, _ := inc.Increment(ctx, "cnt", ttl)
	require.Equal(t, int64(1), n1)
	n2, _ := inc.Increment(ctx, "cnt", ttl)
	require.Equal(t, int64(2), n2)

	// TTL is set only on first hit via ExpireNX; verify key has TTL and after expiry resets
	m.FastForward(2 * time.Second)
	n3, _ := inc.Increment(ctx, "cnt", ttl)
	require.Equal(t, int64(1), n3)
}

func TestRedis_Miniredis_Increment_UsesDefaultTTL(t *testing.T) {
	m := startMiniredis(t)
	s := &testSettings{driver: "redis", addr: m.Addr(), ttl: 1 * time.Second, prefix: "t:"}
	c, err := cache.New(context.Background(), s, nil)
	require.NoError(t, err)
	defer func() { _ = c.(cache.Closer).Close() }()

	inc := c.(cache.Incrementer)
	ctx := context.Background()
	n, _ := inc.Increment(ctx, "cnt", 0)
	require.Equal(t, int64(1), n)
	m.FastForward(2 * time.Second)
	n2, _ := inc.Increment(ctx, "cnt", 0)
	require.Equal(t, int64(1), n2)
}

func TestRedis_Miniredis_Increment_DifferentKeys(t *testing.T) {
	m := startMiniredis(t)
	s := &testSettings{driver: "redis", addr: m.Addr(), ttl: time.Minute, prefix: "t:"}
	c, err := cache.New(context.Background(), s, nil)
	require.NoError(t, err)
	defer func() { _ = c.(cache.Closer).Close() }()

	inc := c.(cache.Incrementer)
	ctx := context.Background()
	a1, _ := inc.Increment(ctx, "a", time.Minute)
	b1, _ := inc.Increment(ctx, "b", time.Minute)
	a2, _ := inc.Increment(ctx, "a", time.Minute)
	require.Equal(t, int64(1), a1)
	require.Equal(t, int64(1), b1)
	require.Equal(t, int64(2), a2)
}

func TestRedis_Miniredis_Close(t *testing.T) {
	m := startMiniredis(t)
	s := &testSettings{driver: "redis", addr: m.Addr(), ttl: time.Minute, prefix: "t:"}
	c, err := cache.New(context.Background(), s, nil)
	require.NoError(t, err)
	require.NoError(t, c.(cache.Closer).Close())
}

func TestRedis_Miniredis_ErrorPaths_AfterServerClose(t *testing.T) {
	m := startMiniredis(t)
	s := &testSettings{driver: "redis", addr: m.Addr(), ttl: time.Minute, prefix: "t:"}
	c, err := cache.New(context.Background(), s, nil)
	require.NoError(t, err)
	// Kill server; redis client will fail on next op.
	m.Close()

	ctx := context.Background()
	// Get should return wrapped error (not redis.Nil)
	_, _, err = c.Get(ctx, "k")
	require.Error(t, err)
	require.Contains(t, err.Error(), "cache: redis get")

	// Set should error
	err = c.Set(ctx, "k", []byte("v"), time.Minute)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cache: redis set")

	// Delete should error
	err = c.Delete(ctx, "k")
	require.Error(t, err)
	require.Contains(t, err.Error(), "cache: redis delete")

	// Increment should error
	inc := c.(cache.Incrementer)
	_, err = inc.Increment(ctx, "cnt", time.Minute)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cache: redis increment")

	// Close should still succeed (closes pool)
	require.NoError(t, c.(cache.Closer).Close())
}
