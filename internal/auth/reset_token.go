package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"transport-app/internal/cache"
)

// ResetTokenStore issues single-use password-reset/email-verify tokens with an
// expiry. Only the HMAC hash of a token is stored, so a memory, cache or
// database leak does not expose usable links.
//
// Tokens live in process memory by default. WithPersistence stores them in
// auth_one_time_tokens (migration 00187) so links survive restarts; the cache
// constructor backs them with a shared cache for multi-replica deployments.
type ResetTokenStore struct {
	mu        sync.Mutex
	ttl       time.Duration
	entries   map[string]resetTokenEntry
	db        *sql.DB
	purpose   string
	cache     cache.Cache
	namespace string
}

type resetTokenEntry struct {
	email   string
	expires time.Time
}

type resetCacheRecord struct {
	Email   string    `json:"email"`
	Expires time.Time `json:"expires"`
}

// NewResetTokenStore creates a process-local store with the given token
// lifetime (defaults to 30 minutes when ttl <= 0). Use
// NewResetTokenStoreWithCache for multi-replica deployments.
func NewResetTokenStore(ttl time.Duration) *ResetTokenStore {
	return NewResetTokenStoreWithCache(ttl, nil)
}

// NewResetTokenStoreWithCache backs tokens with a shared cache so a link
// minted by one replica can be redeemed by another. Nil/no-op caches keep the
// local map (single-instance tier, CACHE_DRIVER=none).
func NewResetTokenStoreWithCache(ttl time.Duration, c cache.Cache) *ResetTokenStore {
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	return &ResetTokenStore{
		ttl:       ttl,
		cache:     sharedCache(c),
		namespace: "auth:reset:",
		entries:   make(map[string]resetTokenEntry),
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

// WithNamespace scopes the cache keys to one purpose (reset vs email verify).
// Stores that share a cache must have distinct namespaces, otherwise a token
// minted for one purpose redeems for the other.
func (s *ResetTokenStore) WithNamespace(ns string) *ResetTokenStore {
	if ns != "" {
		s.namespace = ns
	}
	return s
}

func (s *ResetTokenStore) cacheKey(hash string) string {
	return s.namespace + hash
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

	if s.cache != nil {
		data, err := json.Marshal(resetCacheRecord{
			Email:   email,
			Expires: time.Now().Add(s.ttl),
		})
		if err != nil {
			return "", fmt.Errorf("reset: encode: %w", err)
		}
		if err := s.cache.Set(context.Background(), s.cacheKey(HashToken(token)), data, s.ttl); err != nil {
			return "", fmt.Errorf("reset: cache set: %w", err)
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
	if s.cache != nil {
		ctx := context.Background()
		hash := HashToken(token)
		key := s.cacheKey(hash)

		// Atomic counters (memory, Redis) claim single use across replicas.
		// Other backends fall back to a local lock: weaker, but still safe
		// for the single-process tier.
		inc, atomic := s.cache.(cache.Incrementer)
		if !atomic {
			s.mu.Lock()
			defer s.mu.Unlock()
		}

		raw, ok, err := s.cache.Get(ctx, key)
		if err != nil || !ok {
			return "", false
		}
		var rec resetCacheRecord
		if json.Unmarshal(raw, &rec) != nil || time.Now().After(rec.Expires) {
			return "", false
		}
		// Claim before the purge. Two racing DEL calls both succeed, so only
		// the caller whose atomic increment returns 1 may redeem the token.
		if atomic {
			n, err := inc.Increment(ctx, s.namespace+"claim:"+hash, s.ttl)
			if err != nil || n != 1 {
				return "", false
			}
		}
		// Fail closed: a token we cannot purge must not be redeemed, or a
		// failed delete leaves it replayable.
		if err := s.cache.Delete(ctx, key); err != nil {
			return "", false
		}
		return rec.Email, true
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
