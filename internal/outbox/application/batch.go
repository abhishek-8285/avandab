package application

// Durable mobile outbox batch persistence, extracted from
// internal/handlers/outbox_batch.go (handlers-end-state ADR, Phase 2). The
// handler keeps HTTP decoding/status mapping; this file keeps the single
// transaction insert with idempotency-key primary keys.

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	// ErrOutboxUnavailable reports a transaction/prepare failure (503 retry).
	ErrOutboxUnavailable = errors.New("outbox unavailable")
	// ErrOutboxWriteFailed reports an exec/commit failure (503 retry).
	ErrOutboxWriteFailed = errors.New("outbox write failed")
)

// Command is one queued mobile command.
type Command struct {
	IdempotencyKey string
	Command        string
	Payload        string
}

// Enqueue persists the batch in ONE transaction. The idempotency key is the
// primary key, so replays are no-ops (ON CONFLICT DO NOTHING); every step is
// checked and any failure surfaces as a typed error so the mobile client
// retries instead of silently losing the batch.
func Enqueue(ctx context.Context, db *sql.DB, commands []Command) error {
	if db == nil || len(commands) == 0 {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return ErrOutboxUnavailable
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO outbox_events (id, aggregate_id, aggregate_type, event_type, payload, created_at) VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT DO NOTHING`)
	if err != nil {
		_ = tx.Rollback()
		return ErrOutboxUnavailable
	}
	defer func() { _ = stmt.Close() }()
	var execErr error
	for _, c := range commands {
		id := c.IdempotencyKey
		if id == "" {
			id = uuid.NewString()
		}
		if _, err := stmt.ExecContext(ctx, id, id, "mobile_outbox", c.Command, c.Payload, time.Now().UTC()); err != nil {
			execErr = err
			break
		}
	}
	if execErr == nil {
		if err := tx.Commit(); err != nil {
			execErr = err
		}
	}
	if execErr != nil {
		_ = tx.Rollback()
		return ErrOutboxWriteFailed
	}
	return nil
}
