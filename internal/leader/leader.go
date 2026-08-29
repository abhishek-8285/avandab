// Package leader provides a DB-backed lease lock so background workers
// (crons, sweepers, relays) run on exactly one replica when the app scales
// out. The lease lives in the worker_leases table (migration 00079) using
// BIGINT epoch-millis expiry for SQLite/Postgres/MySQL portability.
package leader

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
	"time"
)

// DefaultTTL is how long a lease stays valid without renewal. A dead
// instance's leases become claimable after this window.
const DefaultTTL = 30 * time.Second

// Manager claims and renews named worker leases against the app database.
type Manager struct {
	db     *sql.DB
	holder string
	ttl    time.Duration
	log    *slog.Logger
}

// NewManager builds a Manager. An empty holder falls back to hostname+pid so
// two replicas on one host still get distinct identities.
func NewManager(db *sql.DB, holder string, ttl time.Duration, log *slog.Logger) *Manager {
	if holder == "" {
		host, _ := os.Hostname()
		holder = fmt.Sprintf("%s-%d", host, os.Getpid())
	}
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	if log == nil {
		log = slog.Default()
	}
	return &Manager{db: db, holder: holder, ttl: ttl, log: log}
}

// tryAcquire atomically claims `name`. The upsert only wins when the current
// lease is expired or already held by us (renewal). Supported identically by
// SQLite and Postgres; MySQL users should switch to the Redis cache driver
// for coordination instead (documented limitation).
func (m *Manager) tryAcquire(ctx context.Context, name string) (bool, error) {
	now := time.Now().UnixMilli()
	expiry := now + m.ttl.Milliseconds()
	const q = `
INSERT INTO worker_leases (name, holder, expires_at)
VALUES (?, ?, ?)
ON CONFLICT(name) DO UPDATE SET
    holder = excluded.holder,
    expires_at = excluded.expires_at
WHERE worker_leases.expires_at < ? OR worker_leases.holder = excluded.holder`
	res, err := m.db.ExecContext(ctx, q, name, m.holder, expiry, now)
	if err != nil {
		return false, fmt.Errorf("leader: acquire %q: %w", name, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("leader: rows affected %q: %w", name, err)
	}
	return n == 1, nil
}

// Release drops the lease if we hold it. Safe to call multiple times.
func (m *Manager) Release(ctx context.Context, name string) {
	_, err := m.db.ExecContext(ctx,
		`DELETE FROM worker_leases WHERE name = ? AND holder = ?`, name, m.holder)
	if err != nil && !errors.Is(err, context.Canceled) {
		m.log.Warn("leader: release failed", "lease", name, "error", err)
	}
}

// RunAsLeader blocks until the lease is acquired (or ctx is done), then runs
// fn with automatic background renewal. If fn returns, the lease is released.
// Exactly one replica across the fleet executes fn at a time.
func (m *Manager) RunAsLeader(ctx context.Context, name string, fn func(ctx context.Context)) {
	for {
		won, err := m.tryAcquire(ctx, name)
		if err != nil {
			m.log.Error("leader: acquire error; retrying", "lease", name, "error", err)
		} else if won {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(m.ttl) / 6):
		}
	}

	// Renewal loop keeps the lease alive while fn runs.
	renewCtx, cancelRenew := context.WithCancel(ctx)
	defer cancelRenew()
	go func() {
		ticker := time.NewTicker(m.ttl / 3)
		defer ticker.Stop()
		for {
			select {
			case <-renewCtx.Done():
				return
			case <-ticker.C:
				won, err := m.tryAcquire(renewCtx, name)
				if err != nil || !won {
					m.log.Error("leader: lease lost mid-run; cancelling work", "lease", name, "error", err)
					cancelRenew()
					return
				}
			}
		}
	}()

	func() {
		defer m.Release(context.WithoutCancel(ctx), name)
		// A panic in any worker must never take down the whole server —
		// recover, log loudly, and release the lease so a replica can retry.
		defer func() {
			if r := recover(); r != nil {
				m.log.Error("leader: worker panicked", "lease", name, "panic", r, "stack", string(debug.Stack()))
			}
		}()
		fn(renewCtx)
	}()
}

// TryRunAsLeader is RunAsLeader without waiting: it runs fn immediately in
// the caller goroutine only when the lease can be claimed right now, and
// leaves renewal to fn's own lifetime. Returns true if fn ran.
func (m *Manager) TryRunAsLeader(ctx context.Context, name string, fn func(ctx context.Context)) bool {
	won, err := m.tryAcquire(ctx, name)
	if err != nil || !won {
		return false
	}
	go func() {
		defer m.Release(context.WithoutCancel(ctx), name)
		defer func() {
			if r := recover(); r != nil {
				m.log.Error("leader: worker panicked", "lease", name, "panic", r, "stack", string(debug.Stack()))
			}
		}()
		fn(ctx)
	}()
	return true
}
