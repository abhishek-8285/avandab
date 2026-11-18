package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// RequireAPIAuth must not panic when constructed with a nil SessionStore and
// the request carries no Authorization header (the cookie fallback path).
func TestRequireAPIAuth_NilStoreCookiePathReturns401(t *testing.T) {
	called := false
	h := RequireAPIAuth(nil, []byte("test-api-secret-at-least-32-bytes"), DefaultTenantResolver)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	rr := httptest.NewRecorder()

	require.NotPanics(t, func() { h.ServeHTTP(rr, req) })
	require.Equal(t, http.StatusUnauthorized, rr.Code)
	require.False(t, called, "next handler must not run on failed auth")
}
