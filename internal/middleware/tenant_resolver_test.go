package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
	"transport-app/internal/cache"
	"transport-app/internal/shared"
)

// fakeTenantCache is an in-memory cache.Cache recording Set calls so tests can
// assert what the resolver persisted.
type fakeTenantCache struct {
	mu     sync.Mutex
	items  map[string][]byte
	sets   int
	getErr error
}

func newFakeTenantCache() *fakeTenantCache {
	return &fakeTenantCache{items: make(map[string][]byte)}
}

func (f *fakeTenantCache) Get(_ context.Context, key string) ([]byte, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return nil, false, f.getErr
	}
	v, ok := f.items[key]
	return v, ok, nil
}

func (f *fakeTenantCache) Set(_ context.Context, key string, value []byte, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sets++
	cp := make([]byte, len(value))
	copy(cp, value)
	f.items[key] = cp
	return nil
}

func (f *fakeTenantCache) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.items, key)
	return nil
}

// countingLookup serves user→(tenant,status) from a map and counts calls.
type countingLookup struct {
	users map[string]struct {
		tenant string
		status string
		err    error
	}
	calls map[string]int
}

func newCountingLookup() *countingLookup {
	return &countingLookup{
		users: make(map[string]struct {
			tenant string
			status string
			err    error
		}),
		calls: make(map[string]int),
	}
}

func (c *countingLookup) add(userID, tenant, status string) {
	c.users[userID] = struct {
		tenant string
		status string
		err    error
	}{tenant, status, nil}
}

func (c *countingLookup) lookup(_ context.Context, userID string) (string, string, error) {
	c.calls[userID]++
	u, ok := c.users[userID]
	if !ok {
		return "", "", errors.New("user not found")
	}
	if u.err != nil {
		return "", "", u.err
	}
	return u.tenant, u.status, nil
}

func TestTenantForUserResolver_ActiveUserCaches(t *testing.T) {
	lk := newCountingLookup()
	lk.add("usr-active", "tenant-a", "active")
	fc := newFakeTenantCache()
	resolver := TenantForUserResolver(lk.lookup, fc)

	tid, err := resolver(context.Background(), "usr-active")
	require.NoError(t, err)
	assert.Equal(t, shared.TenantID("tenant-a"), tid)
	assert.Equal(t, 1, lk.calls["usr-active"])
	assert.Equal(t, 1, fc.sets, "first hit must populate cache")

	// Second call served from cache: no additional SQL lookup.
	tid, err = resolver(context.Background(), "usr-active")
	require.NoError(t, err)
	assert.Equal(t, shared.TenantID("tenant-a"), tid)
	assert.Equal(t, 1, lk.calls["usr-active"], "second call must hit cache, not lookup")
}

func TestTenantForUserResolver_SuspendedRejected(t *testing.T) {
	lk := newCountingLookup()
	lk.add("usr-susp", "tenant-b", "suspended")
	resolver := TenantForUserResolver(lk.lookup, cache.Noop{})

	tid, err := resolver(context.Background(), "usr-susp")
	require.ErrorIs(t, err, auth.ErrTenantSuspended)
	assert.Equal(t, shared.TenantID(""), tid)
}

func TestTenantForUserResolver_LookupErrorPropagates(t *testing.T) {
	lk := newCountingLookup()
	resolver := TenantForUserResolver(lk.lookup, cache.Noop{})

	tid, err := resolver(context.Background(), "usr-missing")
	require.Error(t, err)
	assert.Equal(t, shared.TenantID(""), tid)

	lk.add("usr-dberr", "tenant-c", "active")
	lk.users["usr-dberr"] = struct {
		tenant string
		status string
		err    error
	}{"", "", assert.AnError}
	_, err = resolver(context.Background(), "usr-dberr")
	require.ErrorIs(t, err, assert.AnError)
}

func TestTenantForUserResolver_EmptyTenantIDIsError(t *testing.T) {
	lk := newCountingLookup()
	lk.users["usr-notenant"] = struct {
		tenant string
		status string
		err    error
	}{"", "active", nil}
	resolver := TenantForUserResolver(lk.lookup, cache.Noop{})

	_, err := resolver(context.Background(), "usr-notenant")
	require.Error(t, err)
}

func TestTenantForUserResolver_MalformedCacheFallsBackToLookup(t *testing.T) {
	lk := newCountingLookup()
	lk.add("usr-malformed", "tenant-d", "active")
	fc := newFakeTenantCache()
	require.NoError(t, fc.Set(context.Background(), "usertenant:usr-malformed", []byte("no-separator-garbage"), time.Minute))
	resolver := TenantForUserResolver(lk.lookup, fc)

	tid, err := resolver(context.Background(), "usr-malformed")
	require.NoError(t, err)
	assert.Equal(t, shared.TenantID("tenant-d"), tid)
	assert.Equal(t, 1, lk.calls["usr-malformed"], "malformed payload must fall back to live lookup")
}

// ── Gate matrix ────────────────────────────────────────────────────────────

func TestGateOff_DefaultResolverProceeds(t *testing.T) {
	store := auth.NewSessionStore("gate-off-secret-32-bytes-long-xxx1", false)
	rec := httptest.NewRecorder()
	store.CreateSession(rec, "usr-gateoff", "admin", "Admin")
	cookie := rec.Result().Cookies()[0]

	handler := AuthRequired(store, "/login", DefaultTenantResolver)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, shared.DefaultTenant, shared.TenantIDFromContext(r.Context()))
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(cookie)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestGateOn_SuspendedUser_WebRedirectsWithFlash(t *testing.T) {
	lk := newCountingLookup()
	lk.add("usr-susp-web", "tenant-x", "suspended")
	resolver := TenantForUserResolver(lk.lookup, cache.Noop{})
	store := auth.NewSessionStore("gate-on-secret-32-bytes-long-xxx2", false)
	rec := httptest.NewRecorder()
	store.CreateSession(rec, "usr-susp-web", "admin", "Admin")
	cookie := rec.Result().Cookies()[0]

	handler := AuthRequired(store, "/login", resolver)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("suspended user must not reach protected page")
	}))
	req := httptest.NewRequest("GET", "/dashboard", nil)
	req.AddCookie(cookie)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusSeeOther, rr.Code)
	assert.Equal(t, "/login", rr.Header().Get("Location"))
	flash := ""
	for _, c := range rr.Result().Cookies() {
		if c.Name == "flash_error" {
			flash = c.Value
		}
	}
	assert.Contains(t, flash, "suspended")
}

func TestGateOn_SuspendedUser_APIReturns401WithMessage(t *testing.T) {
	lk := newCountingLookup()
	lk.add("usr-susp-api", "tenant-y", "suspended")
	resolver := TenantForUserResolver(lk.lookup, cache.Noop{})
	secret := []byte("gate-on-api-secret-32-bytes-long-x3")
	store := auth.NewSessionStore(string(secret), false)
	rec := httptest.NewRecorder()
	store.CreateSession(rec, "usr-susp-api", "viewer", "Viewer")
	cookie := rec.Result().Cookies()[0]

	handler := RequireAPIAuth(store, secret, resolver)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("suspended org must not reach API")
	}))
	req := httptest.NewRequest("GET", "/api/v1/trips", nil)
	req.AddCookie(cookie)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Contains(t, rr.Body.String(), "suspended")
}

func TestGateOn_BearerForgedTenantClaimIgnored(t *testing.T) {
	lk := newCountingLookup()
	lk.add("usr-forge", "tenant-real", "active")
	resolver := TenantForUserResolver(lk.lookup, cache.Noop{})
	secret := []byte("forge-secret-32-bytes-long-xxxxxxxx")

	// Token claims a DIFFERENT tenant than the server-side lookup resolves.
	claims := auth.APITokenClaims{UserID: "usr-forge", Role: "admin", TenantID: "tenant-forged"}
	token, err := auth.IssueAPIToken(secret, claims)
	require.NoError(t, err)

	var gotTenant shared.TenantID
	handler := RequireAPIAuth(nil, secret, resolver)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTenant = shared.TenantIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, shared.TenantID("tenant-real"), gotTenant,
		"principal tenant must come from server lookup, not the token's advisory tid")
}

func TestGateOn_BearerSuspendedOrg401(t *testing.T) {
	lk := newCountingLookup()
	lk.add("usr-susp-bearer", "tenant-z", "suspended")
	resolver := TenantForUserResolver(lk.lookup, cache.Noop{})
	secret := []byte("susp-bearer-secret-32-bytes-long-xx4")

	claims := auth.APITokenClaims{UserID: "usr-susp-bearer", Role: "admin"}
	token, err := auth.IssueAPIToken(secret, claims)
	require.NoError(t, err)

	handler := RequireAPIAuth(nil, secret, resolver)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("suspended org must not reach API via bearer either")
	}))
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Contains(t, rr.Body.String(), "suspended")
}
