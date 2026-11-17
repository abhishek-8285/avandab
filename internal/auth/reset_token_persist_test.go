package auth_test

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"transport-app/internal/auth"
)

// TestResetTokenStore_PersistentSingleUseAndPurposeIsolation guards audit
// finding M1 (2026-11-12): reset/verify tokens lived only in process memory, so
// a restart invalidated every outstanding link and a multi-replica deployment
// rejected links issued by another replica. Persisted tokens must also stay
// single-use and must not cross purposes.
func TestResetTokenStore_PersistentSingleUseAndPurposeIsolation(t *testing.T) {
	db := newTokenTestDB(t)
	t.Cleanup(func() { _ = db.Close() })
	auth.SetTokenSecret([]byte("test-token-secret-32-bytes-long!"))

	reset := auth.NewResetTokenStore(time.Hour).WithPersistence(db, "password_reset")
	verify := auth.NewResetTokenStore(time.Hour).WithPersistence(db, "email_verify")

	token, err := reset.Create("user@example.com")
	if err != nil {
		t.Fatalf("create persistent token: %v", err)
	}

	var stored string
	if err := db.QueryRow(`SELECT token_hash FROM auth_one_time_tokens WHERE purpose = 'password_reset'`).Scan(&stored); err != nil {
		t.Fatalf("read stored token: %v", err)
	}
	if stored == token {
		t.Fatal("raw token must never be stored")
	}

	if _, ok := verify.Consume(token); ok {
		t.Fatal("a password-reset token must not redeem as an email-verify token")
	}

	email, ok := reset.Consume(token)
	if !ok || email != "user@example.com" {
		t.Fatalf("persistent token should redeem once, got ok=%v email=%q", ok, email)
	}
	if _, ok := reset.Consume(token); ok {
		t.Fatal("persistent token must be single-use")
	}
}

// TestResetTokenStore_PersistentExpiry proves the database path still rejects
// expired links.
func TestResetTokenStore_PersistentExpiry(t *testing.T) {
	db := newTokenTestDB(t)
	t.Cleanup(func() { _ = db.Close() })
	auth.SetTokenSecret([]byte("test-token-secret-32-bytes-long!"))

	store := auth.NewResetTokenStore(time.Hour).WithPersistence(db, "password_reset")
	token, err := store.Create("expired@example.com")
	if err != nil {
		t.Fatalf("create persistent token: %v", err)
	}
	if _, err := db.Exec(`UPDATE auth_one_time_tokens SET expires_at = $1 WHERE token_hash = $2`,
		time.Now().Add(-time.Minute), auth.HashToken(token)); err != nil {
		t.Fatalf("expire stored token: %v", err)
	}
	if _, ok := store.Consume(token); ok {
		t.Fatal("expired persistent token must not redeem")
	}
}

func newTokenTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "tokens.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE auth_one_time_tokens (
			token_hash TEXT PRIMARY KEY,
			email      TEXT NOT NULL,
			purpose    TEXT NOT NULL,
			expires_at DATETIME NOT NULL,
			created_at DATETIME NOT NULL DEFAULT (datetime('now'))
		)`); err != nil {
		t.Fatalf("create token table: %v", err)
	}
	return db
}
