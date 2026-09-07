// Package experiments implements a small server-side A/B experiment
// framework: deterministic user assignment and best-effort event logging.
package experiments

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

// DashboardExperiment is the dashboard redesign A/B test.
const DashboardExperiment = "dashboard_v2"

// Variants returned by Assign.
const (
	VariantA = "A" // control: existing dashboard
	VariantB = "B" // treatment: charts, KPI cards, alert feed
)

// Assign deterministically assigns a user to a variant. Rollout is the
// percentage (0-100) of users that receive VariantB. Force overrides the
// assignment (used for QA). Assignment is stable per (tenant, user) pair.
func Assign(rollout int, force, tenantID, userID string) string {
	switch force {
	case VariantA:
		return VariantA
	case VariantB:
		return VariantB
	}
	if rollout <= 0 {
		return VariantA
	}
	if rollout >= 100 {
		return VariantB
	}
	h := sha1.Sum([]byte(tenantID + ":" + userID))
	digest := hex.EncodeToString(h[:])
	var bucket uint64
	for i := 0; i < 8; i++ {
		bucket = bucket*256 + uint64(digest[i])
	}
	if int(bucket%100) < rollout {
		return VariantB
	}
	return VariantA
}

// Recorder logs experiment events to the experiment_events table. All
// methods are best-effort: they never fail the caller.
type Recorder struct {
	db *sql.DB
}

// NewRecorder returns a Recorder writing to db. Nil db yields a no-op
// recorder.
func NewRecorder(db *sql.DB) *Recorder {
	return &Recorder{db: db}
}

// Record inserts one event synchronously. Failures are logged, not returned.
func (r *Recorder) Record(ctx context.Context, tenantID, userID, experiment, variant, event string, meta map[string]any) {
	if r == nil || r.db == nil {
		return
	}
	payload := "{}"
	if len(meta) > 0 {
		if b, err := json.Marshal(meta); err == nil {
			payload = string(b)
		}
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO experiment_events (id, tenant_id, user_id, experiment, variant, event, meta, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		newID("evt"), tenantID, userID, experiment, variant, event, payload, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		slog.Warn("experiment: failed to record event", "experiment", experiment, "event", event, "error", err)
	}
}

// RecordAsync records the event on a background goroutine so page renders
// never wait on the write. The request context may be cancelled by the time
// the goroutine runs, so the write uses a detached context with a timeout.
func (r *Recorder) RecordAsync(ctx context.Context, tenantID, userID, experiment, variant, event string, meta map[string]any) {
	if r == nil || r.db == nil {
		return
	}
	detached := context.WithoutCancel(ctx)
	go func() {
		defer func() {
			if recover() != nil {
				slog.Warn("experiment: panic in async event record")
			}
		}()
		writeCtx, cancel := context.WithTimeout(detached, 3*time.Second)
		defer cancel()
		r.Record(writeCtx, tenantID, userID, experiment, variant, event, meta)
	}()
}

func newID(prefix string) string {
	return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UnixNano(), time.Now().UnixMilli()%100000)
}
