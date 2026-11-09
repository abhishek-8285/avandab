package availability

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS availability_samples (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		sampled_at DATETIME NOT NULL DEFAULT (datetime('now')),
		component TEXT NOT NULL DEFAULT 'overall',
		healthy BOOLEAN NOT NULL,
		latency_ms INTEGER NOT NULL DEFAULT 0
	)`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}
	return db
}

func TestLoadSnapshot_NoSamplesIsNotMeasured(t *testing.T) {
	db := testDB(t)
	snap := LoadSnapshot(context.Background(), db, time.Now().UTC(), 30*24*time.Hour, time.Minute)
	if snap.Measured {
		t.Fatal("fresh deployment must report not measured, never a percentage")
	}
	if len(snap.Days) != 30 {
		t.Fatalf("expected 30 history days, got %d", len(snap.Days))
	}
	for _, d := range snap.Days {
		if d.HasData {
			t.Fatalf("day %s must have no data", d.Date)
		}
	}
}

func TestLoadSnapshot_MissingTableIsNotMeasured(t *testing.T) {
	db, err := sql.Open("sqlite", "file:no_table?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	snap := LoadSnapshot(context.Background(), db, time.Now().UTC(), 30*24*time.Hour, time.Minute)
	if snap.Measured {
		t.Fatal("missing table must degrade to not measured")
	}
}

func TestLoadSnapshot_MeasuresUptimeAndDays(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

	// 4 expected ticks at 1-minute spacing; the -1m tick is missing, so the
	// gap must count as downtime: 3 healthy / 4 expected = 75%.
	for _, offset := range []int{-3, -2, 0} {
		at := now.Add(time.Duration(offset) * time.Minute)
		if err := recordAt(ctx, db, ComponentOverall, true, 2*time.Millisecond, at); err != nil {
			t.Fatalf("record: %v", err)
		}
	}
	snap := LoadSnapshot(ctx, db, now, 30*24*time.Hour, time.Minute)
	if !snap.Measured {
		t.Fatal("expected measured snapshot")
	}
	if snap.Samples != 3 || snap.Healthy != 3 {
		t.Fatalf("samples=%d healthy=%d, want 3/3", snap.Samples, snap.Healthy)
	}
	if snap.Expected != 4 {
		t.Fatalf("expected=4 (one missing tick counts as downtime), got %d", snap.Expected)
	}
	if want := 75.0; snap.UptimePct != want {
		t.Fatalf("uptime=%.2f, want %.2f", snap.UptimePct, want)
	}

	// All samples are healthy, so every populated day bucket is 100%. Assert
	// in aggregate so a test running across UTC midnight cannot flake.
	total := 0
	for _, d := range snap.Days {
		total += d.Samples
		if d.HasData && d.UptimePercent != 100 {
			t.Fatalf("healthy-only day must be 100%%, got %+v", d)
		}
	}
	if total != 3 {
		t.Fatalf("day buckets hold %d samples, want 3", total)
	}
}

func TestSampler_RecordsSamplesAndStops(t *testing.T) {
	db := testDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		NewSampler(db, 10*time.Millisecond, nil).Run(ctx)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM availability_samples`).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		if n >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("sampler did not stop on context cancellation")
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM availability_samples`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n == 0 {
		t.Fatal("sampler recorded no samples")
	}
}

func TestPrune_DropsOldSamples(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	old := time.Now().UTC().Add(-120 * 24 * time.Hour)
	if err := recordAt(ctx, db, ComponentOverall, true, time.Millisecond, old); err != nil {
		t.Fatalf("record old: %v", err)
	}
	if err := recordAt(ctx, db, ComponentOverall, true, time.Millisecond, time.Now().UTC()); err != nil {
		t.Fatalf("record fresh: %v", err)
	}
	NewSampler(db, time.Minute, nil).prune(ctx)
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM availability_samples`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 sample after prune, got %d", n)
	}
}
