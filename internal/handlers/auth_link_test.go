package handlers

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"transport-app/internal/config"
)

// Password-reset and verification links must use the configured public origin.
// Before the fix they were built from r.Host / X-Forwarded-Proto, which an
// unauthenticated attacker controls on /forgot-password.
func TestOutOfBandURLIgnoresRequestHost(t *testing.T) {
	h := &AuthHandlers{App: &App{Config: &config.Config{PublicBaseURL: "https://app.example"}}}
	r := httptest.NewRequest("POST", "http://attacker.example/forgot-password", nil)
	r.Host = "attacker.example"
	r.Header.Set("X-Forwarded-Proto", "http")

	got := h.outOfBandURL("/reset-password?token=abc")
	require.Equal(t, "https://app.example/reset-password?token=abc", got)
	require.NotContains(t, got, "attacker.example")
	require.NotContains(t, got, "http://")
}

func TestOutOfBandURLFallsBackToConfiguredDefault(t *testing.T) {
	h := &AuthHandlers{App: &App{Config: &config.Config{}}}
	got := h.outOfBandURL("/verify-email?token=abc")
	require.Equal(t, "https://avandab.com/verify-email?token=abc", got)
}
