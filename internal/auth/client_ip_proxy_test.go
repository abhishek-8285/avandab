package auth_test

import (
	"net/http/httptest"
	"testing"

	"transport-app/internal/auth"
)

// TestClientIP_IgnoresSpoofedProxyHeadersByDefault: a loopback reverse proxy
// does not sanitise client-supplied headers, so trusting them from any private
// peer lets a caller rotate rate-limit keys at will (M80).
func TestClientIP_IgnoresSpoofedProxyHeadersByDefault(t *testing.T) {
	t.Setenv("TRUST_PROXY", "")
	t.Setenv("TRUST_PROXY_CIDRS", "")
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:4567"
	req.Header.Set("CF-Connecting-IP", "1.2.3.4")
	if got := auth.ClientIP(req); got != "127.0.0.1" {
		t.Fatalf("ClientIP = %q, want 127.0.0.1 (spoofed header ignored)", got)
	}
}

func TestClientIP_ExplicitProxyCIDRs(t *testing.T) {
	t.Setenv("TRUST_PROXY", "")
	t.Setenv("TRUST_PROXY_CIDRS", "127.0.0.0/8, 10.0.0.0/8")
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:4567"
	req.Header.Set("CF-Connecting-IP", "1.2.3.4")
	if got := auth.ClientIP(req); got != "1.2.3.4" {
		t.Fatalf("ClientIP = %q, want 1.2.3.4 for configured proxy", got)
	}
}
