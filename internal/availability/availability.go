// Package availability records and reports platform uptime samples.
//
// The public /status page used to display invented availability numbers
// (99.98% uptime, per-subsystem percentages). This package replaces those
// with real samples written by an in-process sampler and read back for the
// 30-day history (AGENTS.md P0 "stop fake data", trust surface P5.2).
package availability

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// ComponentOverall is the platform-wide health signal recorded by the sampler.
const ComponentOverall = "overall"

// retention is how long raw samples are kept. The status page only needs 30
// days; 90 gives headroom for incident review without unbounded growth.
const retention = 90 * 24 * time.Hour

// Day is one UTC day of measured availability.
type Day struct {
	Date          string  `json:"date"`
	UptimePercent float64 `json:"uptime_percent"`
	Samples       int     `json:"samples"`
	Healthy       int     `json:"healthy"`
	HasData       bool    `json:"has_data"`
}

// Snapshot is the measured availability window returned to the status page.
// Measured is false when no samples exist yet (fresh deployment): callers must
// render "not measured" instead of inventing a percentage.
type Snapshot struct {
	Measured  bool
	UptimePct float64
	Samples   int
	Healthy   int
	Expected  int
	Days      []Day
}

// Record inserts one availability sample for the given component at now.
func Record(ctx context.Context, db *sql.DB, component string, healthy bool, latency time.Duration) error {
	return recordAt(ctx, db, component, healthy, latency, time.Now().UTC())
}

func recordAt(ctx context.Context, db *sql.DB, component string, healthy bool, latency time.Duration, at time.Time) error {
	if db == nil {
		return fmt.Errorf("availability: nil db")
	}
	if component == "" {
		component = ComponentOverall
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO availability_samples (sampled_at, component, healthy, latency_ms)
		VALUES ($1, $2, $3, $4)`,
		at.UTC(), component, healthy, latency.Milliseconds())
	return err
}

// LoadSnapshot aggregates the last window of samples. Missing ticks between
// the first and last sample count as downtime (the process was not sampling),
// so a crash lowers the measured percentage instead of hiding in the gaps.
// A missing table (old DB before migration 00180) reports Measured=false
// rather than failing the status page.
func LoadSnapshot(ctx context.Context, db *sql.DB, now time.Time, window, interval time.Duration) Snapshot {
	snap := Snapshot{Days: emptyDays(now, window)}
	if db == nil || window <= 0 {
		return snap
	}
	if interval <= 0 {
		interval = time.Minute
	}

	rows, err := db.QueryContext(ctx, `
		SELECT sampled_at, healthy
		FROM availability_samples
		WHERE component = $1 AND sampled_at >= $2
		ORDER BY sampled_at`, ComponentOverall, now.UTC().Add(-window))
	if err != nil {
		return snap
	}
	defer func() { _ = rows.Close() }()

	type point struct {
		at      time.Time
		healthy bool
	}
	var points []point
	for rows.Next() {
		var at time.Time
		var healthy bool
		if err := rows.Scan(&at, &healthy); err != nil {
			continue
		}
		points = append(points, point{at: at.UTC(), healthy: healthy})
	}
	if err := rows.Err(); err != nil || len(points) == 0 {
		return snap
	}

	span := points[len(points)-1].at.Sub(points[0].at)
	expected := int(span/interval) + 1
	if expected < len(points) {
		expected = len(points)
	}
	healthy := 0
	for _, p := range points {
		if p.healthy {
			healthy++
		}
	}

	snap.Measured = true
	snap.Samples = len(points)
	snap.Healthy = healthy
	snap.Expected = expected
	snap.UptimePct = float64(healthy) / float64(expected) * 100

	for _, p := range points {
		date := p.at.Format("2006-01-02")
		for i := range snap.Days {
			if snap.Days[i].Date != date {
				continue
			}
			snap.Days[i].Samples++
			if p.healthy {
				snap.Days[i].Healthy++
			}
			snap.Days[i].HasData = true
			break
		}
	}
	for i := range snap.Days {
		if snap.Days[i].Samples > 0 {
			snap.Days[i].UptimePercent = float64(snap.Days[i].Healthy) / float64(snap.Days[i].Samples) * 100
		}
	}
	return snap
}

// emptyDays returns one Day per UTC date in the window, oldest first.
func emptyDays(now time.Time, window time.Duration) []Day {
	days := int(window.Hours() / 24)
	if days <= 0 {
		return nil
	}
	end := now.UTC()
	out := make([]Day, 0, days)
	for i := days - 1; i >= 0; i-- {
		out = append(out, Day{Date: end.AddDate(0, 0, -i).Format("2006-01-02")})
	}
	return out
}

// Sampler probes the platform on an interval and records availability samples.
type Sampler struct {
	db       *sql.DB
	interval time.Duration
	logger   *slog.Logger
}

// NewSampler builds a sampler. A nil/zero interval falls back to 1 minute.
func NewSampler(db *sql.DB, interval time.Duration, logger *slog.Logger) *Sampler {
	if interval <= 0 {
		interval = time.Minute
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Sampler{db: db, interval: interval, logger: logger}
}

// Run blocks until ctx is cancelled: one probe immediately, then every
// interval, pruning samples older than the retention window every 6 hours.
func (s *Sampler) Run(ctx context.Context) {
	if s == nil || s.db == nil {
		return
	}
	s.sample(ctx)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	lastPrune := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sample(ctx)
			if time.Since(lastPrune) >= 6*time.Hour {
				s.prune(ctx)
				lastPrune = time.Now()
			}
		}
	}
}

func (s *Sampler) sample(ctx context.Context) {
	start := time.Now()
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	healthy := s.db.PingContext(pingCtx) == nil
	if err := Record(ctx, s.db, ComponentOverall, healthy, time.Since(start)); err != nil {
		s.logger.Warn("availability sample not recorded", "error", err)
	}
}

func (s *Sampler) prune(ctx context.Context) {
	cutoff := time.Now().UTC().Add(-retention)
	if _, err := s.db.ExecContext(ctx, `DELETE FROM availability_samples WHERE sampled_at < $1`, cutoff); err != nil {
		s.logger.Warn("availability prune failed", "error", err)
	}
}
