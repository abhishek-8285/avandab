package auth_test

import (
	"net/http/httptest"
	"testing"
	"time"

	"transport-app/internal/auth"
)

// TestSessionStore_AppliesConfiguredCookieSecurity guards audit findings M2
// (2026-11-12): the store accepted `secure` but CreateSessionWithToken always
// emitted Secure=true with MaxAge=86400, ignoring both the constructor flag
// and Config.SessionMaxAge. Plain-HTTP staging logins failed, and a configured
// session lifetime had no effect.
func TestSessionStore_AppliesConfiguredCookieSecurity(t *testing.T) {
	const secret = "test-secret-key-32-bytes-long-!"

	store := auth.NewSessionStore(secret, false)
	store.SetMaxAge(2 * time.Hour)

	rec := httptest.NewRecorder()
	store.CreateSession(rec, "usr-1", "admin", "Admin")
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected one session cookie, got %d", len(cookies))
	}
	if cookies[0].Secure {
		t.Fatal("development cookie with secure=false must not carry Secure")
	}
	if cookies[0].MaxAge != 7200 {
		t.Fatalf("expected configured MaxAge 7200, got %d", cookies[0].MaxAge)
	}

	secureStore := auth.NewSessionStore(secret, true)
	secureRec := httptest.NewRecorder()
	secureStore.CreateSession(secureRec, "usr-1", "admin", "Admin")
	if !secureRec.Result().Cookies()[0].Secure {
		t.Fatal("HTTPS cookie with secure=true must carry Secure")
	}
}
