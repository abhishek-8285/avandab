package auth

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"transport-app/internal/cache"
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
// not leak usable codes. With a shared cache (Redis) codes and resend
// throttles work across replicas; without one the maps stay process-local.
type OTPStore struct {
	mu         sync.Mutex
	ttl        time.Duration
	cache      cache.Cache
	lastSentAt map[string]time.Time
	entries    map[string]otpEntry
}

type otpEntry struct {
	codeHash string
	expires  time.Time
	attempts int
}

// otpCacheRecord is the JSON form stored in the shared cache. Re-sends need
// the last-send time as well as the code, so both live under one key.
type otpCacheRecord struct {
	CodeHash string    `json:"code_hash"`
	Expires  time.Time `json:"expires"`
	Attempts int       `json:"attempts"`
	LastSent time.Time `json:"last_sent"`
}

// sharedCache normalizes a backend for transient auth state. Noop discards
// everything, so treat it like "no shared store" and keep the local fallback.
func sharedCache(c cache.Cache) cache.Cache {
	if c == nil {
		return nil
	}
	if _, ok := c.(cache.Noop); ok {
		return nil
	}
	return c
}

// NewOTPStore creates a process-local store with the given code lifetime
// (defaults to OTPDefaultTTL when ttl <= 0). Multi-replica deployments must
// use NewOTPStoreWithCache.
func NewOTPStore(ttl time.Duration) *OTPStore {
	return NewOTPStoreWithCache(ttl, nil)
}

// NewOTPStoreWithCache backs issue/verify with a shared cache so codes and
// resend throttles survive across replicas. Nil/no-op caches fall back to the
// local maps (single-instance tier, CACHE_DRIVER=none).
func NewOTPStoreWithCache(ttl time.Duration, c cache.Cache) *OTPStore {
	if ttl <= 0 {
		ttl = OTPDefaultTTL
	}
	return &OTPStore{
		ttl:        ttl,
		cache:      sharedCache(c),
		lastSentAt: make(map[string]time.Time),
		entries:    make(map[string]otpEntry),
	}
}

func otpCacheKey(phone string) string {
	return "auth:otp:" + HashToken(phone)
}

// otpClaimKey is a per-code, atomically incremented marker. Only the caller
// whose increment returns 1 may consume the code; concurrent verifiers see 2+
// and are rejected. Keying on the code hash gives every freshly issued code a
// new marker, so a resend is never blocked by an earlier redemption.
func otpClaimKey(codeHash string) string {
	return "auth:otp:claim:" + codeHash
}

// cacheTTL keeps the throttle window alive even for short-lived test codes.
func (s *OTPStore) cacheTTL() time.Duration {
	if s.ttl < OTPResendThrottle {
		return OTPResendThrottle
	}
	return s.ttl
}

// Create generates a 6-digit code for the phone and returns the raw code (to
// embed in the SMS text). Only its HMAC hash is stored. Re-sends are
// throttled: a second call within OTPResendThrottle rejects with an error so
// a spammer cannot burn free-tier SMS credits.
func (s *OTPStore) Create(phone string) (string, error) {
	if phone == "" {
		return "", fmt.Errorf("otp: phone required")
	}
	if s.cache != nil {
		return s.createCached(phone)
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
	if s.cache != nil {
		return s.verifyCached(phone, code)
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
	if s.cache != nil {
		return s.resendBlockedCached(phone)
	}
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

func (s *OTPStore) createCached(phone string) (string, error) {
	ctx := context.Background()
	key := otpCacheKey(phone)

	if raw, ok, err := s.cache.Get(ctx, key); err != nil {
		return "", fmt.Errorf("otp: cache get: %w", err)
	} else if ok {
		var rec otpCacheRecord
		if json.Unmarshal(raw, &rec) == nil && time.Since(rec.LastSent) < OTPResendThrottle {
			return "", fmt.Errorf("otp: resend blocked, try again in %s", time.Until(rec.LastSent.Add(OTPResendThrottle)).Round(time.Second))
		}
	}

	code, err := randomOTP()
	if err != nil {
		return "", err
	}
	now := time.Now()
	data, err := json.Marshal(otpCacheRecord{
		CodeHash: HashToken(code),
		Expires:  now.Add(s.ttl),
		LastSent: now,
	})
	if err != nil {
		return "", fmt.Errorf("otp: encode: %w", err)
	}
	if err := s.cache.Set(ctx, key, data, s.cacheTTL()); err != nil {
		return "", fmt.Errorf("otp: cache set: %w", err)
	}
	return code, nil
}

func (s *OTPStore) verifyCached(phone, code string) bool {
	ctx := context.Background()
	key := otpCacheKey(phone)

	// Backends with atomic counters (memory, Redis) claim single use across
	// replicas. A custom backend without one falls back to a local lock: that
	// cannot stop a cross-replica race, but it prevents replay in this process.
	inc, atomic := s.cache.(cache.Incrementer)
	if !atomic {
		s.mu.Lock()
		defer s.mu.Unlock()
	}

	raw, ok, err := s.cache.Get(ctx, key)
	if err != nil || !ok {
		return false
	}
	var rec otpCacheRecord
	if json.Unmarshal(raw, &rec) != nil {
		return false
	}
	if time.Now().After(rec.Expires) {
		_ = s.cache.Delete(ctx, key)
		return false
	}

	rec.Attempts++
	if rec.Attempts > OTPMaxAttempts {
		_ = s.cache.Delete(ctx, key)
		return false
	}
	data, err := json.Marshal(rec)
	if err == nil {
		err = s.cache.Set(ctx, key, data, s.cacheTTL())
	}
	if err != nil {
		// Never leave a code usable after a failed attempt write.
		_ = s.cache.Delete(ctx, key)
		return false
	}
	if !CompareToken(code, rec.CodeHash) {
		return false
	}
	// Claim the code before deleting it. The read/modify/write above is not
	// atomic, so without this step two replicas could both match the same
	// code and both return success. Only one increment can return 1.
	if atomic {
		n, err := inc.Increment(ctx, otpClaimKey(rec.CodeHash), s.cacheTTL())
		if err != nil || n != 1 {
			return false
		}
	}
	// Fail closed: if the one-time code cannot be purged, it must not be
	// accepted, otherwise a failed delete makes the code replayable.
	if err := s.cache.Delete(ctx, key); err != nil {
		return false
	}
	return true
}

func (s *OTPStore) resendBlockedCached(phone string) time.Duration {
	raw, ok, err := s.cache.Get(context.Background(), otpCacheKey(phone))
	if err != nil || !ok {
		return 0
	}
	var rec otpCacheRecord
	if json.Unmarshal(raw, &rec) != nil {
		return 0
	}
	remaining := time.Until(rec.LastSent.Add(OTPResendThrottle))
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
