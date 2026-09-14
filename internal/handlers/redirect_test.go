package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSafeRedirect_OnlyReturnsSameOriginPaths(t *testing.T) {
	const host = "app.example.com"
	newReq := func() *http.Request {
		return httptest.NewRequest(http.MethodGet, "https://"+host+"/trips", nil)
	}

	cases := []struct {
		name     string
		target   string
		fallback string
		want     string
	}{
		{"empty falls back", "", "/trips", "/trips"},
		{"plain path kept", "/trips/123", "/trips", "/trips/123"},
		{"path with query kept", "/trips/123?tab=costs", "/trips", "/trips/123?tab=costs"},

		// Open-redirect payloads must never survive.
		{"protocol relative blocked", "//evil.com/x", "/trips", "/trips"},
		{"backslash protocol relative blocked", "/\\evil.com/x", "/trips", "/trips"},
		{"absolute other host blocked", "https://evil.com/x", "/trips", "/trips"},
		{"scheme relative with userinfo blocked", "https://app.example.com@evil.com/", "/trips", "/trips"},

		// Same-host absolute URLs are reduced to their path.
		{"same host absolute reduced", "https://" + host + "/trips/9", "/trips", "/trips/9"},
		{"same host case-insensitive", "https://APP.EXAMPLE.COM/trips/9", "/trips", "/trips/9"},

		{"relative without leading slash blocked", "trips/123", "/trips", "/trips"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := safeRedirect(newReq(), tc.target, tc.fallback)
			if got != tc.want {
				t.Errorf("safeRedirect(%q) = %q, want %q", tc.target, got, tc.want)
			}
		})
	}
}

// The realistic attack: a malicious page links into the app so Referer points
// off-site. The handler must not bounce the user there.
func TestSafeRedirect_RefererAttack(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "https://app.example.com/action", nil)
	r.Header.Set("Referer", "https://evil.example.com/steal")

	got := safeRedirect(r, r.Header.Get("Referer"), "/vehicles/v1")
	if got != "/vehicles/v1" {
		t.Errorf("attacker-controlled Referer leaked into redirect: got %q", got)
	}

	r.Header.Set("Referer", "https://app.example.com/vehicles/v1")
	if got := safeRedirect(r, r.Header.Get("Referer"), "/"); got != "/vehicles/v1" {
		t.Errorf("legitimate same-origin Referer should be preserved: got %q", got)
	}
}
