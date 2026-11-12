package application

// Focused tests for the extracted outbox batch (handlers-end-state ADR,
// Phase 2): single-transaction persistence + idempotency-key replay.

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func sweepOutboxDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("sweep_outbox_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	require.NoError(t, err)
	_, err = db.Exec(`
		CREATE TABLE outbox_events (id TEXT PRIMARY KEY, aggregate_id TEXT, aggregate_type TEXT,
		 event_type TEXT, payload TEXT, created_at DATETIME)`)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestSweepOutbox_EnqueueAndIdempotentReplay(t *testing.T) {
	db := sweepOutboxDB(t)
	ctx := context.Background()
	commands := []Command{
		{IdempotencyKey: "cmd-1", Command: "pod.upload", Payload: "{}"},
		{Command: "expense.log", Payload: `{"amount":100}`},
	}
	require.NoError(t, Enqueue(ctx, db, commands))

	var n int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM outbox_events`).Scan(&n))
	assert.Equal(t, 2, n, "one row per command; empty key gets a generated uuid")
	var eventType, aggregateType string
	require.NoError(t, db.QueryRow(`SELECT event_type, aggregate_type FROM outbox_events WHERE id = 'cmd-1'`).Scan(&eventType, &aggregateType))
	assert.Equal(t, "pod.upload", eventType)
	assert.Equal(t, "mobile_outbox", aggregateType)

	// Replay: PRIMARY KEY id = idempotency key -> no duplicates.
	require.NoError(t, Enqueue(ctx, db, []Command{{IdempotencyKey: "cmd-1", Command: "pod.upload"}}))
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM outbox_events`).Scan(&n))
	assert.Equal(t, 2, n)

	assert.NoError(t, Enqueue(ctx, nil, commands))
	assert.NoError(t, Enqueue(ctx, db, nil))
}
