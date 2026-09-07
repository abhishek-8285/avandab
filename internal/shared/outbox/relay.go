package outbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"transport-app/internal/events"
)

const (
	DefaultPollInterval = 5 * time.Second
	maxAttempts         = 5
)

// Relay polls the outbox table and dispatches unpublished events to the
// event bus, marking them published afterwards.
type Relay struct {
	db          *sql.DB
	bus         events.EventBus
	logger      *slog.Logger
	interval    time.Duration
	mu          sync.Mutex
	attempts    map[string]int
	nextAttempt map[string]time.Time
}

// NewRelay constructs an outbox relay. bus may be nil, in which case
// events are marked published without dispatch.
func NewRelay(db *sql.DB, bus events.EventBus, logger *slog.Logger) *Relay {
	return &Relay{
		db:          db,
		bus:         bus,
		logger:      logger,
		interval:    DefaultPollInterval,
		attempts:    make(map[string]int),
		nextAttempt: make(map[string]time.Time),
	}
}

// Run blocks until ctx is cancelled, polling the outbox table on an interval.
func (r *Relay) Run(ctx context.Context) {
	if r.db == nil {
		r.logger.Warn("outbox relay disabled: nil database")
		return
	}
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	r.logger.Info("outbox relay started", "interval", r.interval.String())

	r.dispatch(ctx)
	for {
		select {
		case <-ctx.Done():
			r.logger.Info("outbox relay stopped")
			return
		case <-ticker.C:
			r.dispatch(ctx)
		}
	}
}

type pendingEvent struct {
	id, aggregateID, aggregateType, eventType, payload string
}

func (r *Relay) dispatch(ctx context.Context) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, aggregate_id, aggregate_type, event_type, payload FROM outbox_events WHERE published_at IS NULL`)
	if err != nil {
		r.logger.Error("outbox relay: failed to query pending events", "error", err)
		return
	}
	defer rows.Close()

	var pending []pendingEvent
	for rows.Next() {
		var e pendingEvent
		if err := rows.Scan(&e.id, &e.aggregateID, &e.aggregateType, &e.eventType, &e.payload); err != nil {
			r.logger.Error("outbox relay: failed to scan row", "error", err)
			return
		}
		pending = append(pending, e)
	}
	if err := rows.Err(); err != nil {
		r.logger.Error("outbox relay: row iteration failed", "error", err)
		return
	}

	for _, e := range pending {
		if r.shouldSkip(e.id) {
			continue
		}
		if err := r.publish(ctx, e); err != nil {
			r.markFailed(e.id, e.eventType)
			continue
		}
		r.markPublished(e.id)
	}
}

func (r *Relay) publish(ctx context.Context, e pendingEvent) error {
	var payload any
	if err := json.Unmarshal([]byte(e.payload), &payload); err != nil {
		r.logger.Error("outbox relay: invalid payload", "id", e.id, "event_type", e.eventType, "error", err)
		return err
	}

	if r.bus != nil {
		r.bus.Publish(ctx, events.Event{Type: e.eventType, Payload: payload})
	} else {
		r.logger.Info("outbox relay: no event bus, marking event published", "id", e.id, "event_type", e.eventType)
	}

	res, err := r.db.ExecContext(ctx, `UPDATE outbox_events SET published_at = $1 WHERE id = $2 AND published_at IS NULL`, time.Now(), e.id)
	if err != nil {
		r.logger.Error("outbox relay: failed to mark event published", "id", e.id, "error", err)
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		r.logger.Warn("outbox relay: event already published elsewhere", "id", e.id)
	}
	return nil
}

func (r *Relay) shouldSkip(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t, ok := r.nextAttempt[id]; ok && time.Now().Before(t) {
		return true
	}
	return false
}

func (r *Relay) markFailed(id, eventType string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.attempts[id]++
	if r.attempts[id] >= maxAttempts {
		r.logger.Error("outbox relay: event dead-lettered after max attempts", "id", id, "event_type", eventType, "attempts", r.attempts[id])
		delete(r.attempts, id)
		delete(r.nextAttempt, id)
		return
	}
	r.nextAttempt[id] = time.Now().Add(r.interval * time.Duration(1<<r.attempts[id]))
}

func (r *Relay) markPublished(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.attempts, id)
	delete(r.nextAttempt, id)
}
