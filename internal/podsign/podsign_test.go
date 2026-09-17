package podsign

import (
	"net/url"
	"strconv"
	"testing"
	"time"
)

// newSigner builds a signer with a fixed clock so expiry is deterministic.
func newSigner(t *testing.T, ttl time.Duration, now time.Time) *Signer {
	t.Helper()
	s, err := New([]byte("0123456789abcdef0123456789abcdef"), ttl)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	s.clock = func() time.Time { return now }
	return s
}

func TestSign_Verify_RoundTrip(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	s := newSigner(t, 6*time.Hour, now)

	signed, err := s.Sign("/uploads/pod/abc123.jpg")
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	u, _ := url.Parse(signed)
	if u.Path != "/uploads/pod/abc123.jpg" {
		t.Fatalf("path = %q, want /uploads/pod/abc123.jpg", u.Path)
	}

	filename, err := s.Verify(u.Path, u.RawQuery)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if filename != "abc123.jpg" {
		t.Fatalf("filename = %q, want abc123.jpg", filename)
	}
}

func TestVerify_RejectsUnsigned(t *testing.T) {
	s := newSigner(t, 6*time.Hour, time.Now())

	// Bare path, no exp/sig — the pre-fix attack: anonymous direct hit.
	if _, err := s.Verify("/uploads/pod/abc123.jpg", ""); err != ErrInvalidSignature {
		t.Fatalf("unsigned URL: err = %v, want ErrInvalidSignature", err)
	}
	if _, err := s.Verify("/uploads/pod/abc123.jpg", "exp=123"); err != ErrInvalidSignature {
		t.Fatalf("missing sig: err = %v, want ErrInvalidSignature", err)
	}
}

func TestVerify_RejectsTamperedExpiry(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	s := newSigner(t, 6*time.Hour, now)

	signed, err := s.Sign("photo.jpg")
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	u, _ := url.Parse(signed)

	// Attacker extends the expiry by 30 days; signature no longer matches.
	exp := now.Add(30 * 24 * time.Hour).Unix()
	v := url.Values{}
	v.Set("exp", strconv.FormatInt(exp, 10))
	v.Set("sig", u.Query().Get("sig"))
	if _, err := s.Verify(u.Path, v.Encode()); err != ErrInvalidSignature {
		t.Fatalf("tampered exp: err = %v, want ErrInvalidSignature", err)
	}
}

func TestVerify_RejectsExpired(t *testing.T) {
	start := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	issued := newSigner(t, 6*time.Hour, start)

	signed, err := issued.Sign("photo.jpg")
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	u, _ := url.Parse(signed)

	// Same secret, 7 hours later.
	later := newSigner(t, 6*time.Hour, start.Add(7*time.Hour))
	if _, err := later.Verify(u.Path, u.RawQuery); err != ErrExpired {
		t.Fatalf("expired URL: err = %v, want ErrExpired", err)
	}
}

func TestVerify_RejectsTraversal(t *testing.T) {
	s := newSigner(t, 6*time.Hour, time.Now())
	for _, bad := range []string{
		"/uploads/pod/../../etc/passwd",
		"/uploads/pod/sub/dir/x.jpg",
		"/uploads/pod/..%2f..%2fetc/passwd",
	} {
		if _, err := s.Verify(bad, ""); err != ErrInvalidSignature {
			t.Fatalf("traversal %q: err = %v, want ErrInvalidSignature", bad, err)
		}
	}
}

func TestNew_RejectsShortKey(t *testing.T) {
	if _, err := New([]byte("short"), time.Hour); err == nil {
		t.Fatal("New with short key should fail closed")
	}
}

func TestSign_AcceptsBareFilenameAndPath(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	s := newSigner(t, 6*time.Hour, now)

	signed, err := s.Sign("bare.jpg")
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	u, _ := url.Parse(signed)
	if _, err := s.Verify(u.Path, u.RawQuery); err != nil {
		t.Fatalf("bare filename round trip: %v", err)
	}
}

func TestSignURLOrRaw_NilSafePassthrough(t *testing.T) {
	var nilSigner *Signer
	if got := nilSigner.SignURLOrRaw("/uploads/pod/a.jpg"); got != "/uploads/pod/a.jpg" {
		t.Fatalf("nil signer: got %q, want passthrough", got)
	}
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	s := newSigner(t, 6*time.Hour, now)
	if got := s.SignURLOrRaw(""); got != "" {
		t.Fatalf("empty input: got %q, want empty", got)
	}
	signed := s.SignURLOrRaw("/uploads/pod/a.jpg")
	u, _ := url.Parse(signed)
	if _, err := s.Verify(u.Path, u.RawQuery); err != nil {
		t.Fatalf("signed output must verify: %v", err)
	}
}
