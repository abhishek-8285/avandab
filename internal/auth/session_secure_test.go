package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// COOKIE_SECURE controls the Secure attribute on both the session cookie and
// its clearing twin. With the flag off (plain-HTTP dev) the attribute must be
// absent; with it on, present.
func TestSessionStoreCookieSecureHonorsConfig(t *testing.T) {
	for _, secure := range []bool{false, true} {
		t.Run(map[bool]string{false: "insecure", true: "secure"}[secure], func(t *testing.T) {
			store := NewSessionStore("test-cookie-secret-at-least-32-bytes-long-0001", secure)

			createRR := httptest.NewRecorder()
			store.CreateSession(createRR, "user-1", "viewer", "User One")
			clearRR := httptest.NewRecorder()
			store.ClearSession(clearRR)

			for label, rr := range map[string]*httptest.ResponseRecorder{"create": createRR, "clear": clearRR} {
				var got *http.Cookie
				for _, c := range rr.Result().Cookies() {
					if c.Name == "session" {
						got = c
					}
				}
				require.NotNil(t, got, "%s must set the session cookie", label)
				require.Equal(t, secure, got.Secure, "%s cookie Secure must honor config", label)
			}
		})
	}
}
