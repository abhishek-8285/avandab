package auth_test

import (
	"net/http/httptest"
	"testing"

	"transport-app/internal/auth"
)

// TestSessionStore_RejectsTokenlessCookieWithValidator pins the fix for the
// forgeable-session bypass: when a server-side validator is configured, a
// signed cookie without a session token must be rejected — its role claim is
// client-controlled and cannot be verified against the session store.
func TestSessionStore_RejectsTokenlessCookieWithValidator(t *testing.T) {
	secret := "test-secret-key-32-bytes-long-!"
	store := auth.NewSessionStore(secret, false)
	mv := &mockValidator{validToken: "token-123"}
	store.SetValidator(mv)

	// CreateSession mints a cookie with no token (legacy/attacker shape).
	rec := httptest.NewRecorder()
	store.CreateSession(rec, "usr-attacker", "admin", "Forged Admin")

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(rec.Result().Cookies()[0])

	data, ok := store.ValidateSession(req)
	if ok {
		t.Fatalf("expected tokenless cookie to be rejected when validator is configured, got ok=true data=%+v", data)
	}

	// Sanity: without a validator, the tokenless cookie still validates
	// (e.g. unit-test stores) — behavior is unchanged for that configuration.
	bare := auth.NewSessionStore(secret, false)
	rec2 := httptest.NewRecorder()
	bare.CreateSession(rec2, "usr-1", "viewer", "V")
	req2 := httptest.NewRequest("GET", "/", nil)
	req2.AddCookie(rec2.Result().Cookies()[0])
	if _, ok := bare.ValidateSession(req2); !ok {
		t.Fatalf("expected tokenless cookie to validate when no validator is configured")
	}
}
