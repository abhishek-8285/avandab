// Package retention enforces the published business-data retention schedule
// (GET /api/v1/privacy/retention-schedule). Telemetry/sessions/outbox already
// have dedicated purgers; this worker covers the business tables that had no
// automated disposal: video telematics, audit logs, breach ledger, consents,
// and DSAR requests.
package retention

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// Target is one table + time column with its maximum age.
type Target struct {
	Label      string
	Table      string
	TimeColumn string
	MaxAge     time.Duration
}

// DefaultTargets mirror the published DPDP retention schedule. Tax invoices /
// financial ledgers are deliberately absent: 8-year statutory retention is
// enforced by the DSAR layer, never by a blanket purge.
func DefaultTargets() []Target {
	return []Target{
		{Label: "video_safety_events", Table: "video_safety_events", TimeColumn: "event_time", MaxAge: 90 * 24 * time.Hour},
		{Label: "video_livestream_sessions", Table: "video_livestream_sessions", TimeColumn: "started_at", MaxAge: 90 * 24 * time.Hour},
		{Label: "audit_logs", Table: "audit_logs", TimeColumn: "created_at", MaxAge: 3 * 365 * 24 * time.Hour},
		{Label: "breach_incidents", Table: "breach_incidents", TimeColumn: "detected_at", MaxAge: 3 * 365 * 24 * time.Hour},
		{Label: "user_consents", Table: "user_consents", TimeColumn: "created_at", MaxAge: 5 * 365 * 24 * time.Hour},
		{Label: "privacy_dsar_requests", Table: "privacy_dsar_requests", TimeColumn: "created_at", MaxAge: 5 * 365 * 24 * time.Hour},
	}
}

// Worker purges expired business rows in bounded batches.
type Worker struct {
	db       *sql.DB
	logger   *slog.Logger
	interval time.Duration
	batch    int
	targets  []Target
	now      func() time.Time
}

// NewWorker builds the retention worker (24h interval, 5,000-row batches).
func NewWorker(db *sql.DB, logger *slog.Logger, interval time.Duration) *Worker {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{db: db, logger: logger, interval: interval, batch: 5000, targets: DefaultTargets(), now: time.Now}
}

// Run purges immediately then once per interval until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) {
	if w == nil || w.db == nil {
		return
	}
	w.purgeLogged(ctx)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.purgeLogged(ctx)
		}
	}
}

func (w *Worker) purgeLogged(ctx context.Context) {
	counts, err := w.PurgeOnce(ctx)
	if err != nil {
		w.logger.Warn("business retention purge failed", "error", err)
		return
	}
	for label, n := range counts {
		if n > 0 {
			w.logger.Info("business retention purged expired rows", "table", label, "rows", n)
		}
	}
}

// PurgeOnce deletes every expired row across all targets and returns per-table
// counts. Safe to call repeatedly (idempotent).
func (w *Worker) PurgeOnce(ctx context.Context) (map[string]int64, error) {
	counts := make(map[string]int64, len(w.targets))
	now := w.now().UTC()
	for _, t := range w.targets {
		cutoff := now.Add(-t.MaxAge)
		n, err := w.purgeTable(ctx, t, cutoff)
		if err != nil {
			return counts, fmt.Errorf("purge %s: %w", t.Table, err)
		}
		counts[t.Label] = n
	}
	return counts, nil
}

func (w *Worker) purgeTable(ctx context.Context, t Target, cutoff time.Time) (int64, error) {
	var total int64
	for {
		res, err := w.db.ExecContext(ctx,
			fmt.Sprintf(`DELETE FROM %s WHERE id IN (
				SELECT id FROM %s WHERE %s < $1 LIMIT $2)`, t.Table, t.Table, t.TimeColumn),
			cutoff, w.batch)
		if err != nil {
			return total, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return total, err
		}
		total += n
		if n < int64(w.batch) {
			return total, nil
		}
	}
}
