package handlers

import (
	"net/http/httptest"
	"testing"
)

// TestRequestScheme_ForwardedProtoRequiresTrustedProxy guards audit finding M8
// (2026-11-12): requestScheme honored X-Forwarded-Proto from any caller, so an
// attacker could make password-reset links downgrade to http://. The header is
// now read only from a trusted proxy and only when its value is http/https.
func TestRequestScheme_ForwardedProtoRequiresTrustedProxy(t *testing.T) {
	t.Setenv("TRUST_PROXY", "false")

	trusted := httptest.NewRequest("GET", "/reset-password", nil)
	trusted.Header.Set("X-Forwarded-Proto", "https")
	if got := requestScheme(trusted); got != "https" {
		t.Fatalf("trusted test proxy should yield https, got %q", got)
	}

	spoofed := httptest.NewRequest("GET", "/reset-password", nil)
	spoofed.RemoteAddr = "8.8.8.8:443"
	spoofed.Header.Set("X-Forwarded-Proto", "https")
	if got := requestScheme(spoofed); got != "http" {
		t.Fatalf("a public client must not control the scheme, got %q", got)
	}

	invalid := httptest.NewRequest("GET", "/reset-password", nil)
	invalid.Header.Set("X-Forwarded-Proto", "javascript")
	if got := requestScheme(invalid); got != "http" {
		t.Fatalf("only http/https are accepted, got %q", got)
	}

	tlsReq := httptest.NewRequest("GET", "https://example.com/reset-password", nil)
	if got := requestScheme(tlsReq); got != "https" {
		t.Fatalf("TLS request must be https, got %q", got)
	}
}
