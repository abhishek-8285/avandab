package auth

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// ResetTokenStore issues single-use password-reset/email-verify tokens with an
// expiry. Only the HMAC hash of a token is stored, so a memory or database
// leak does not expose usable links.
//
// By default tokens live in process memory (single-replica convenience). Call
// WithPersistence to store them in auth_one_time_tokens (migration 00187), so
// links survive restarts and work across replicas.
type ResetTokenStore struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]resetTokenEntry
	db      *sql.DB
	purpose string
}

type resetTokenEntry struct {
	email   string
	expires time.Time
}

// NewResetTokenStore creates a store with the given token lifetime
// (defaults to 30 minutes when ttl <= 0).
func NewResetTokenStore(ttl time.Duration) *ResetTokenStore {
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	return &ResetTokenStore{
		ttl:     ttl,
		entries: make(map[string]resetTokenEntry),
	}
}

// WithPersistence makes the store use the auth_one_time_tokens table instead
// of process memory. purpose isolates password-reset tokens from
// email-verification tokens that share the table. A nil db or empty purpose
// leaves the store in-memory.
func (s *ResetTokenStore) WithPersistence(db *sql.DB, purpose string) *ResetTokenStore {
	if db != nil && purpose != "" {
		s.db = db
		s.purpose = purpose
	}
	return s
}

// Create generates a fresh token for an email and returns the raw token to
// embed in a link. Only the hash is stored.
func (s *ResetTokenStore) Create(email string) (string, error) {
	token, err := GenerateSecureToken()
	if err != nil {
		return "", err
	}
	now := time.Now()
	expires := now.Add(s.ttl)

	if s.db != nil {
		// Opportunistic cleanup: expired rows are useless and the
		// (purpose, expires_at) index keeps this bounded. A cleanup failure
		// must not block issuing the token, but it must not vanish silently.
		if _, err := s.db.ExecContext(context.Background(),
			`DELETE FROM auth_one_time_tokens WHERE expires_at < $1`, now); err != nil {
			slog.Warn("one-time token cleanup failed", "purpose", s.purpose, "error", err)
		}
		if _, err := s.db.ExecContext(context.Background(),
			`INSERT INTO auth_one_time_tokens (token_hash, email, purpose, expires_at)
			 VALUES ($1, $2, $3, $4)`,
			HashToken(token), email, s.purpose, expires); err != nil {
			return "", err
		}
		return token, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked(now)
	s.entries[HashToken(token)] = resetTokenEntry{
		email:   email,
		expires: expires,
	}
	return token, nil
}

// Consume redeems a token exactly once, returning the associated email.
// Expired or unknown tokens report a miss. The DELETE ... RETURNING is atomic,
// so two replicas cannot both redeem the same link.
func (s *ResetTokenStore) Consume(token string) (string, bool) {
	if s.db != nil {
		var email string
		var expires time.Time
		err := s.db.QueryRowContext(context.Background(),
			`DELETE FROM auth_one_time_tokens
			 WHERE token_hash = $1 AND purpose = $2
			 RETURNING email, expires_at`,
			HashToken(token), s.purpose).Scan(&email, &expires)
		if err != nil {
			if !errors.Is(err, sql.ErrNoRows) {
				slog.Error("one-time token consume failed", "purpose", s.purpose, "error", err)
			}
			return "", false
		}
		if time.Now().After(expires) {
			return "", false
		}
		return email, true
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	s.sweepLocked(now)

	key := HashToken(token)
	entry, ok := s.entries[key]
	delete(s.entries, key)
	if !ok || now.After(entry.expires) {
		return "", false
	}
	return entry.email, true
}

func (s *ResetTokenStore) sweepLocked(now time.Time) {
	for key, entry := range s.entries {
		if now.After(entry.expires) {
			delete(s.entries, key)
		}
	}
}
