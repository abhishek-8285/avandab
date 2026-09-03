package auth

import (
	"crypto/rand"
	"fmt"
	"sync"
	"time"
)

const (
	// OTPDefaultTTL — a code is valid for 5 minutes.
	OTPDefaultTTL = 5 * time.Minute
	// OTPMaxAttempts — 5 wrong tries invalidate the code (brute-force guard).
	OTPMaxAttempts = 5
	// OTPResendThrottle — 30s minimum between sends to the same phone.
	OTPResendThrottle = 30 * time.Second
)

// OTPStore issues and verifies one-time SMS passcodes. Only the HMAC hash of
// a code is kept (same hardening as ResetTokenStore), so a memory dump does
// not leak usable codes. In-memory for the single-instance tier; a
// multi-replica deployment must back this with a shared store (Redis/SQL).
type OTPStore struct {
	mu         sync.Mutex
	ttl        time.Duration
	lastSentAt map[string]time.Time
	entries    map[string]otpEntry
}

type otpEntry struct {
	codeHash string
	expires  time.Time
	attempts int
}

// NewOTPStore creates a store with the given code lifetime (defaults to
// OTPDefaultTTL when ttl <= 0).
func NewOTPStore(ttl time.Duration) *OTPStore {
	if ttl <= 0 {
		ttl = OTPDefaultTTL
	}
	return &OTPStore{
		ttl:        ttl,
		lastSentAt: make(map[string]time.Time),
		entries:    make(map[string]otpEntry),
	}
}

// Create generates a 6-digit code for the phone and returns the raw code (to
// embed in the SMS text). Only its HMAC hash is stored. Re-sends are
// throttled: a second call within OTPResendThrottle rejects with an error so
// a spammer cannot burn free-tier SMS credits.
func (s *OTPStore) Create(phone string) (string, error) {
	if phone == "" {
		return "", fmt.Errorf("otp: phone required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked(time.Now())

	if last, ok := s.lastSentAt[phone]; ok && time.Since(last) < OTPResendThrottle {
		return "", fmt.Errorf("otp: resend blocked, try again in %s", time.Until(last.Add(OTPResendThrottle)).Round(time.Second))
	}

	code, err := randomOTP()
	if err != nil {
		return "", err
	}
	s.entries[phone] = otpEntry{
		codeHash: HashToken(code),
		expires:  time.Now().Add(s.ttl),
	}
	s.lastSentAt[phone] = time.Now()
	return code, nil
}

// Verify checks a submitted code against the stored hash, enforcing expiry,
// a max-attempt budget, and constant-time comparison. Success clears the
// entry (single-use). The throttle note is also cleared so a verified phone
// can immediately request a new code.
func (s *OTPStore) Verify(phone, code string) bool {
	if phone == "" || code == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked(time.Now())

	entry, ok := s.entries[phone]
	if !ok {
		return false
	}
	if time.Now().After(entry.expires) {
		delete(s.entries, phone)
		return false
	}
	entry.attempts++
	if entry.attempts > OTPMaxAttempts {
		delete(s.entries, phone)
		delete(s.lastSentAt, phone)
		return false
	}
	s.entries[phone] = entry
	if !CompareToken(code, entry.codeHash) {
		return false
	}
	delete(s.entries, phone)
	delete(s.lastSentAt, phone)
	return true
}

// ResendBlockedFor reports how long a re-send to phone stays throttled
// (0 means a new code may be issued immediately). Purely informational.
func (s *OTPStore) ResendBlockedFor(phone string) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	last, ok := s.lastSentAt[phone]
	if !ok {
		return 0
	}
	remaining := time.Until(last.Add(OTPResendThrottle))
	if remaining < 0 {
		return 0
	}
	return remaining
}

func (s *OTPStore) sweepLocked(now time.Time) {
	for phone, entry := range s.entries {
		if now.After(entry.expires) {
			delete(s.entries, phone)
			delete(s.lastSentAt, phone)
		}
	}
}

// randomOTP returns a 6-digit code from crypto/rand (no math/rand seed risk).
func randomOTP() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("otp: random source failed: %w", err)
	}
	n := int(b[0])<<24 | int(b[1])<<16 | int(b[2])<<8 | int(b[3])
	if n < 0 {
		n = -n
	}
	return fmt.Sprintf("%06d", n%1000000), nil
}
