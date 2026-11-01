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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var ran atomic.Bool
	done := make(chan struct{})
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		m.RunAsLeader(ctx, "worker_x", func(c context.Context) {
			ran.Store(true)
			close(done)
			<-c.Done() // hold lease until cancelled
		})
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("fn never ran")
	}
	cancel()
	<-workerDone
}

func TestRunAsLeader_RecoversFromWorkerPanicAndReleasesLease(t *testing.T) {
	db := testDB(t)
	m := leader.NewManager(db, "panic-inst", 2*time.Second, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	panicked := make(chan struct{})
	panicDone := make(chan struct{})
	go func() {
		defer close(panicDone)
		m.RunAsLeader(ctx, "panic_job", func(ctx context.Context) {
			close(panicked)
			panic("boom")
		})
	}()

	select {
	case <-panicked:
	case <-time.After(3 * time.Second):
		t.Fatal("fn never ran")
	}
	<-panicDone

	// After the panic is recovered the lease must be released, so a fresh
	// claim by the same or another instance can win it again.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		tryDone := make(chan struct{})
		if m.TryRunAsLeader(context.Background(), "panic_job", func(ctx context.Context) {
			close(tryDone)
		}) {
			<-tryDone
			time.Sleep(20 * time.Millisecond)
			return // lease released despite the panic
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("lease not released after worker panic")
}

func TestRunAsLeader_SecondInstanceWaits(t *testing.T) {
	db := testDB(t)
	release := make(chan struct{})
	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	m1 := leader.NewManager(db, "a", 5*time.Second, slog.Default())
	m1Done := make(chan struct{})
	go func() {
		defer close(m1Done)
		m1.RunAsLeader(ctx1, "job", func(ctx context.Context) {
			<-release // hold the lease until we let go
		})
	}()

	// Give m1 time to claim.
	time.Sleep(200 * time.Millisecond)

	var ranB atomic.Bool
	m2 := leader.NewManager(db, "b", 5*time.Second, slog.Default())
	ctx2, cancel2 := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel2()
	m2Done := make(chan struct{})
	go func() {
		defer close(m2Done)
		m2.RunAsLeader(ctx2, "job", func(ctx context.Context) { ranB.Store(true) })
	}()

	<-m2Done
	if ranB.Load() {
		t.Fatal("second instance ran while first still held the lease")
	}
	close(release)
	<-m1Done
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var ran atomic.Bool
	done := make(chan struct{})
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		m1.RunAsLeader(ctx, "stale", func(c context.Context) {
			ran.Store(true)
			close(done)
			<-c.Done()
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
	cancel()
	<-workerDone
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
