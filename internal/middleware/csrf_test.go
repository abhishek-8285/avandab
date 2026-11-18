package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"transport-app/internal/auth"
)

// sessionRequest builds a POST request carrying a valid session cookie.
func sessionRequest(t *testing.T, store *auth.SessionStore, method, target string, origin string) *http.Request {
	t.Helper()

	rec := httptest.NewRecorder()
	store.CreateSession(rec, "usr-1", "admin", "Admin User")
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatalf("expected session cookie")
	}

	req := httptest.NewRequest(method, target, nil)
	req.AddCookie(cookies[0])
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	return req
}

func doCSRF(mw func(http.Handler) http.Handler, req *http.Request) int {
	rec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rec, req)
	return rec.Code
}

func TestCSRFProtect_LenientAllowsMissingHeaders(t *testing.T) {
	store := auth.NewSessionStore("csrf-test-secret-32-bytes!!", false)
	mw := CSRFProtect(store)

	// curl-style: session cookie, no Origin/Referer -> allowed by default.
	req := sessionRequest(t, store, http.MethodPost, "/api/x", "")
	if code := doCSRF(mw, req); code != http.StatusOK {
		t.Fatalf("expected lenient mode to allow missing headers, got %d", code)
	}

	// Mismatched origin is still rejected.
	req2 := sessionRequest(t, store, http.MethodPost, "/api/x", "https://evil.example.com")
	if code := doCSRF(mw, req2); code != http.StatusForbidden {
		t.Fatalf("expected mismatched origin to be rejected, got %d", code)
	}
}

// A private/loopback peer is not a trusted proxy by default, so it cannot
// rewrite the origin check with X-Forwarded-Host.
func TestCSRFStrict_IgnoresForwardedHostFromUntrustedPeer(t *testing.T) {
	t.Setenv("TRUST_PROXY", "")
	t.Setenv("TRUST_PROXY_CIDRS", "")

	store := auth.NewSessionStore("csrf-test-secret-32-bytes!!", false)
	req := sessionRequest(t, store, http.MethodPost, "/api/x", "https://evil.example.com")
	req.Host = "app.example.com"
	req.RemoteAddr = "192.168.1.20:1234"
	req.Header.Set("X-Forwarded-Host", "evil.example.com")

	if code := doCSRF(CSRFProtectStrict(store), req); code != http.StatusForbidden {
		t.Fatalf("spoofed X-Forwarded-Host accepted from untrusted peer: got %d, want 403", code)
	}
}

func TestCSRFProtectStrict_RejectsMissingHeaders(t *testing.T) {
	store := auth.NewSessionStore("csrf-test-secret-32-bytes!!", false)
	mw := CSRFProtectStrict(store)

	req := sessionRequest(t, store, http.MethodPost, "/api/x", "")
	if code := doCSRF(mw, req); code != http.StatusForbidden {
		t.Fatalf("expected strict mode to reject missing headers, got %d", code)
	}

	// Same-origin header passes.
	req2 := sessionRequest(t, store, http.MethodPost, "/api/x", "http://example.com")
	req2.Host = "example.com"
	if code := doCSRF(mw, req2); code != http.StatusOK {
		t.Fatalf("expected same-origin request to pass, got %d", code)
	}

	// Referer counts as an origin indicator.
	req3 := sessionRequest(t, store, http.MethodPost, "/api/x", "")
	req3.Host = "example.com"
	req3.Header.Set("Referer", "http://example.com/page")
	if code := doCSRF(mw, req3); code != http.StatusOK {
		t.Fatalf("expected Referer to satisfy strict mode, got %d", code)
	}
}

func TestCSRFProtect_Bypasses(t *testing.T) {
	store := auth.NewSessionStore("csrf-test-secret-32-bytes!!", false)
	mw := CSRFProtectStrict(store)

	// Bearer-token clients are exempt even in strict mode.
	req := sessionRequest(t, store, http.MethodPost, "/api/x", "")
	req.Header.Set("Authorization", "Bearer some-token")
	if code := doCSRF(mw, req); code != http.StatusOK {
		t.Fatalf("expected Bearer client to bypass CSRF check, got %d", code)
	}

	// Safe methods are exempt.
	req2 := sessionRequest(t, store, http.MethodGet, "/", "")
	if code := doCSRF(mw, req2); code != http.StatusOK {
		t.Fatalf("expected GET to bypass CSRF check, got %d", code)
	}
}

func TestSameOrigin_UsesXForwardedHost(t *testing.T) {
	// Behind an explicitly trusted reverse proxy the internal Host differs
	// from the browser origin; httptest's peer alone is not trusted.
	t.Setenv("TRUST_PROXY", "true")
	t.Setenv("TRUST_PROXY_CIDRS", "")
	req := httptest.NewRequest(http.MethodPost, "http://internal.svc/api/x", nil)
	req.Host = "internal.svc"
	req.Header.Set("X-Forwarded-Host", "public.example.com")
	req.Header.Set("Origin", "https://public.example.com")

	if !sameOrigin(req) {
		t.Fatalf("expected origin to match X-Forwarded-Host")
	}

	// Without X-Forwarded-Host the origin must match r.Host.
	req2 := httptest.NewRequest(http.MethodPost, "http://internal.svc/api/x", nil)
	req2.Header.Set("Origin", "https://public.example.com")
	if sameOrigin(req2) {
		t.Fatalf("expected mismatched origin to be rejected without X-Forwarded-Host")
	}

	// Missing origin and referer is allowed (lenient behavior).
	req3 := httptest.NewRequest(http.MethodPost, "http://internal.svc/api/x", nil)
	if !sameOrigin(req3) {
		t.Fatalf("expected missing origin/referer to be allowed in lenient comparison")
	}
}
