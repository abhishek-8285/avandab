package middleware

import "testing"

// TestRateLimitDisabled_IgnoredOutsideDevelopment guards audit finding M6
// (2026-11-12): RATE_LIMIT_DISABLED=1 or AVANDAB_TEST_MODE=1 switched off every
// limiter, including /login and /api/v1/auth/token, with no environment check.
// A leaked or mistaken production env var disabled brute-force protection.
// The test switches now only work where APP_ENV is unset or development.
func TestRateLimitDisabled_IgnoredOutsideDevelopment(t *testing.T) {
	t.Setenv("RATE_LIMIT_DISABLED", "1")
	t.Setenv("AVANDAB_TEST_MODE", "1")

	t.Setenv("APP_ENV", "production")
	if rateLimitDisabled() {
		t.Fatal("production must never honor test rate-limit switches")
	}

	t.Setenv("APP_ENV", "staging")
	if rateLimitDisabled() {
		t.Fatal("staging must never honor test rate-limit switches")
	}

	t.Setenv("APP_ENV", "development")
	if !rateLimitDisabled() {
		t.Fatal("development should honor the documented test switches")
	}

	t.Setenv("APP_ENV", "testing")
	if !rateLimitDisabled() {
		t.Fatal("testing should honor the harness switches")
	}

	t.Setenv("APP_ENV", "")
	if !rateLimitDisabled() {
		t.Fatal("unset APP_ENV documents to development and should honor the switches")
	}
}
