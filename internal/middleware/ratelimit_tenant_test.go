package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"transport-app/internal/cache"
	"transport-app/internal/middleware"
	"transport-app/internal/shared"
)

func tenantReq(tenant string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/trips", nil)
	if tenant != "" {
		req = req.WithContext(shared.ContextWithTenantID(req.Context(), shared.TenantID(tenant)))
	}
	return req
}

// One tenant's flood must not consume another tenant's budget: per-tenant
// aggregate caps the noisy neighbor where per-IP limits cannot.
func TestRateLimitTenantDistributed_PerTenantBudgets(t *testing.T) {
	mc, err := cache.New(nil2ctx(), &fakeSettings{}, nil)
	if err != nil {
		t.Fatalf("cache.New: %v", err)
	}
	mw := middleware.RateLimitTenantDistributed(mc, 2)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))

	serve := func(tenant string) int {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, tenantReq(tenant))
		return rec.Code
	}

	if c := serve("acme"); c != 200 {
		t.Fatalf("acme req 1 = %d, want 200", c)
	}
	if c := serve("acme"); c != 200 {
		t.Fatalf("acme req 2 = %d, want 200", c)
	}
	if c := serve("acme"); c != 429 {
		t.Errorf("acme req 3 = %d, want 429", c)
	}
	// Other tenant unaffected by acme's flood.
	if c := serve("beta"); c != 200 {
		t.Errorf("beta req 1 = %d, want 200", c)
	}
}

func TestRateLimitTenantDistributed_NoTenantPassthrough(t *testing.T) {
	mc, err := cache.New(nil2ctx(), &fakeSettings{}, nil)
	if err != nil {
		t.Fatalf("cache.New: %v", err)
	}
	mw := middleware.RateLimitTenantDistributed(mc, 1)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))

	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, tenantReq(""))
		if rec.Code != 200 {
			t.Fatalf("tenantless req %d = %d, want 200 (passthrough to per-IP limiters)", i+1, rec.Code)
		}
	}
}

func TestRateLimitTenantDistributed_NoIncrementerStandsDown(t *testing.T) {
	mw := middleware.RateLimitTenantDistributed(cache.Noop{}, 1)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))

	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, tenantReq("acme"))
		if rec.Code != 200 {
			t.Fatalf("req %d = %d, want 200 (no atomic backend, must not enforce)", i+1, rec.Code)
		}
	}
}
