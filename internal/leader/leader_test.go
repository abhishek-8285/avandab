package leader_test

import (
	"context"
	"database/sql"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"transport-app/internal/leader"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+t.TempDir()+"/leases.db?mode=rwc")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE worker_leases (
		name TEXT PRIMARY KEY, holder TEXT NOT NULL, expires_at INTEGER NOT NULL)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	return db
}

func TestRunAsLeader_RunsFnOnceLeaseAcquired(t *testing.T) {
	db := testDB(t)
	m := leader.NewManager(db, "instance-a", 2*time.Second, slog.Default())

	var ran atomic.Bool
	done := make(chan struct{})
	go func() {
		m.RunAsLeader(context.Background(), "worker_x", func(ctx context.Context) {
			ran.Store(true)
			close(done)
			<-ctx.Done() // hold lease until cancelled
		})
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("fn never ran")
	}
}

func TestRunAsLeader_RecoversFromWorkerPanicAndReleasesLease(t *testing.T) {
	db := testDB(t)
	m := leader.NewManager(db, "panic-inst", 2*time.Second, slog.Default())

	panicked := make(chan struct{})
	go m.RunAsLeader(context.Background(), "panic_job", func(ctx context.Context) {
		close(panicked)
		panic("boom")
	})

	select {
	case <-panicked:
	case <-time.After(3 * time.Second):
		t.Fatal("fn never ran")
	}

	// After the panic is recovered the lease must be released, so a fresh
	// claim by the same or another instance can win it again.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if m.TryRunAsLeader(context.Background(), "panic_job", func(ctx context.Context) {}) {
			return // lease released despite the panic
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("lease not released after worker panic")
}

func TestRunAsLeader_SecondInstanceWaits(t *testing.T) {
	db := testDB(t)
	release := make(chan struct{})
	m1 := leader.NewManager(db, "a", 5*time.Second, slog.Default())
	go m1.RunAsLeader(context.Background(), "job", func(ctx context.Context) {
		<-release // hold the lease until we let go
	})

	// Give m1 time to claim.
	time.Sleep(200 * time.Millisecond)

	var ranB atomic.Bool
	m2 := leader.NewManager(db, "b", 5*time.Second, slog.Default())
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		m2.RunAsLeader(ctx, "job", func(ctx context.Context) { ranB.Store(true) })
	}()

	time.Sleep(600 * time.Millisecond)
	if ranB.Load() {
		t.Fatal("second instance ran while first still held the lease")
	}
	close(release)
}

func TestRunAsLeader_TakesOverAfterExpiry(t *testing.T) {
	db := testDB(t)
	m1 := leader.NewManager(db, "a", 300*time.Millisecond, slog.Default())
	// Simulate a dead holder: seed an expired lease directly.
	if _, err := db.Exec(
		`INSERT INTO worker_leases (name, holder, expires_at) VALUES ('stale', 'dead', ?)`,
		time.Now().Add(-time.Hour).UnixMilli()); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var ran atomic.Bool
	done := make(chan struct{})
	go func() {
		m1.RunAsLeader(context.Background(), "stale", func(ctx context.Context) {
			ran.Store(true)
			close(done)
			<-ctx.Done()
		})
	}()

	select {
	case <-done:
		if !ran.Load() {
			t.Fatal("fn flag not set despite running")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expired lease was never taken over")
	}
}

func TestRelease_RemovesOwnLeaseOnly(t *testing.T) {
	db := testDB(t)
	a := leader.NewManager(db, "a", time.Minute, slog.Default())
	b := leader.NewManager(db, "b", time.Minute, slog.Default())

	if _, err := db.Exec(`INSERT INTO worker_leases (name, holder, expires_at) VALUES ('j', 'b', ?)`,
		time.Now().Add(time.Minute).UnixMilli()); err != nil {
		t.Fatalf("seed: %v", err)
	}

	a.Release(context.Background(), "j") // wrong holder — no-op
	var holder string
	if err := db.QueryRow(`SELECT holder FROM worker_leases WHERE name='j'`).Scan(&holder); err != nil || holder != "b" {
		t.Fatalf("lease held by %q (err=%v), want b untouched", holder, err)
	}

	b.Release(context.Background(), "j")
	err := db.QueryRow(`SELECT holder FROM worker_leases WHERE name='j'`).Scan(&holder)
	if err != sql.ErrNoRows {
		t.Fatalf("lease still present after own release (err=%v)", err)
	}
}
