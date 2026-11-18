package auth_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"transport-app/internal/auth"
)

type failingValidator struct{ err error }

func (v *failingValidator) ValidateSessionToken(context.Context, string) (*auth.SessionData, error) {
	return nil, v.err
}
func (v *failingValidator) RevokeSessionToken(context.Context, string) error { return v.err }
func (v *failingValidator) ValidateAPITokenUser(context.Context, string) (string, bool, error) {
	return "", false, v.err
}

// TestRevokeSession_FailsClosed proves that a storage error during logout is
// surfaced and the cookie is kept — otherwise the user believes they signed
// out while the token stays live for its full lifetime.
func TestRevokeSession_FailsClosed(t *testing.T) {
	secret := "test-secret-key-that-is-at-least-32-chars-long"
	store := auth.NewSessionStore(secret, false)
	store.SetValidator(&failingValidator{err: errors.New("SQLITE_BUSY")})

	createRec := httptest.NewRecorder()
	store.CreateSessionWithToken(createRec, "usr-1", "viewer", "User", "token-1")
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(createRec.Result().Cookies()[0])

	rec := httptest.NewRecorder()
	if err := store.RevokeSession(req, rec); err == nil {
		t.Fatal("expected revocation error to be returned")
	}
	for _, c := range rec.Result().Cookies() {
		if c.MaxAge == -1 {
			t.Fatal("cookie cleared despite failed server-side revocation")
		}
	}
}
