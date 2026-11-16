package service

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"transport-app/internal/repository/sqlite"
)

// TestLoginThrottle_LocksAndClears guards audit finding M3 (2026-11-12):
// only per-IP login limits existed, so credential stuffing from many IPs
// bypassed them. The per-account throttle must lock after repeated failures,
// clear on success, and release an expired lock.
func TestLoginThrottle_LocksAndClears(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "throttle.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`
		CREATE TABLE login_throttle (
			email_key    TEXT PRIMARY KEY,
			failures     INTEGER NOT NULL DEFAULT 0,
			locked_until DATETIME,
			updated_at   DATETIME NOT NULL DEFAULT (datetime('now'))
		)`); err != nil {
		t.Fatalf("create throttle table: %v", err)
	}

	svc := &AuthService{baseService: baseService{
		store: sqlite.NewRepository(db),
		log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}}
	ctx := context.Background()
	const email = "Target@Example.com"

	for i := 0; i < loginMaxFailures; i++ {
		svc.recordLoginFailure(ctx, email)
	}
	if !svc.loginLocked(ctx, email) {
		t.Fatalf("account should be locked after %d failures", loginMaxFailures)
	}

	svc.clearLoginFailures(ctx, email)
	if svc.loginLocked(ctx, email) {
		t.Fatal("successful login must clear the lock")
	}

	for i := 0; i < loginMaxFailures; i++ {
		svc.recordLoginFailure(ctx, email)
	}
	if _, err := db.Exec(`UPDATE login_throttle SET locked_until = $1 WHERE email_key = $2`,
		time.Now().Add(-time.Minute), loginThrottleKey(email)); err != nil {
		t.Fatalf("expire lock: %v", err)
	}
	if svc.loginLocked(ctx, email) {
		t.Fatal("an expired lock must not block login")
	}
}
