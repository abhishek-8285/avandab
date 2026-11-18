package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"transport-app/internal/config"
)

func TestStartupSecretsError_DevNeedsExplicitOptIn(t *testing.T) {
	cfg := &config.Config{
		AppEnv:         "development",
		CookieSecret:   "dev-secret-32bytes-for-cookie-signing!",
		APITokenSecret: "",
	}
	if err := startupSecretsError(cfg, false); err == nil {
		t.Fatal("expected dev default secrets to be refused without ALLOW_DEV_SECRETS=1")
	}
	if err := startupSecretsError(cfg, true); err != nil {
		t.Fatalf("expected explicit local opt-in to be allowed, got %v", err)
	}
	cfg.APITokenSecret = strings.Repeat("a", 40)
	cfg.CookieSecret = strings.Repeat("b", 40)
	if err := startupSecretsError(cfg, false); err != nil {
		t.Fatalf("expected strong secrets to pass, got %v", err)
	}
}

// A production deployment on committed default secrets must be refused by
// run(), which returns an error instead of calling os.Exit directly. Returning
// through main is what lets the startup defers (signal stop, DB close, cache
// close) execute on every failure path.
func TestRunFailsClosedOnKnownDefaultSecrets(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("API_SECRET", "")
	t.Setenv("COOKIE_SECRET", "dev-secret-key-change-in-production-32b!")

	err := run()
	if err == nil {
		t.Fatal("expected run to reject known default secrets in production")
	}
	if !strings.Contains(err.Error(), "known default secrets") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// The generic /uploads/* mount must never serve the signature-only POD
// directory, including when a doubled slash or encoded segment routes around
// the dedicated /uploads/pod/* handler.
func TestTenantFileGuardRejectsPODPaths(t *testing.T) {
	for _, p := range []string{"/pod/x.jpg", "/pod/sub/x.jpg", "/POD/x.jpg", "/%70od/x.jpg"} {
		called := false
		h := tenantFileGuard(nil, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
		req := httptest.NewRequest(http.MethodGet, "/uploads"+p, nil)
		req.URL.Path = p
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if called {
			t.Fatalf("path %q reached the uploads file server", p)
		}
		if rec.Code != http.StatusNotFound {
			t.Fatalf("path %q code = %d, want 404", p, rec.Code)
		}
	}
}
