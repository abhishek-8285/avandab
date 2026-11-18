package auth

import (
	"bytes"
	"testing"
)

// A binary that hashes a token before NewSessionStore runs must not fall back
// to a publicly known HMAC key. The fallback is random per process instead.
func TestRandomTokenSecretFallbackIsNotHardcoded(t *testing.T) {
	a := randomTokenSecret()
	b := randomTokenSecret()
	if len(a) != 32 {
		t.Fatalf("expected 32 bytes, got %d", len(a))
	}
	if bytes.Equal(a, b) {
		t.Fatal("two fallback secrets must not match")
	}
	if string(a) == "transport-app-fallback-token-secret-change-me" {
		t.Fatal("hardcoded token HMAC fallback present")
	}
}
