package outbox

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"transport-app/internal/events"
)

type relayTestEvent struct {
	Hello string `json:"hello"`
}

func TestRelayDispatchesAndMarksPublished(t *testing.T) {
	// Named shared-cache memory DB: plain ":memory:" gives every pooled
	// connection a PRIVATE empty database, so under parallel load the
	// writer and the relay can land on different conns (missing table →
	// skipped ticks → 2s deadline flake). Single connection for determinism.
	db, err := sql.Open("sqlite", "file:relay_dispatch_test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE outbox_events (
		id TEXT PRIMARY KEY,
		aggregate_id TEXT NOT NULL,
		aggregate_type TEXT NOT NULL,
		event_type TEXT NOT NULL,
		payload TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP NOT NULL,
		published_at DATETIME
	)`); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := NewOutboxWriter(db).SaveEvents(ctx, "agg-1", "booking", []any{relayTestEvent{Hello: "world"}}); err != nil {
		t.Fatal(err)
	}

	bus := events.NewInMemoryBus()
	var got events.Event
	bus.Subscribe("relayTestEvent", func(ctx context.Context, e events.Event) error {
		got = e
		return nil
	})

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	relay := NewRelay(db, bus, logger)
	relay.interval = 10 * time.Millisecond

	runCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() { relay.Run(runCtx); close(done) }()

	var published sql.NullTime
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		err := db.QueryRow(`SELECT published_at FROM outbox_events`).Scan(&published)
		if err == nil && published.Valid {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done

	if !published.Valid {
		t.Fatal("event was not marked published")
	}
	if got.Type != "relayTestEvent" {
		t.Fatalf("expected event type relayTestEvent, got %s", got.Type)
	}
	payload, ok := got.Payload.(map[string]interface{})
	if !ok || payload["hello"] != "world" {
		t.Fatalf("unexpected payload: %#v", got.Payload)
	}
}
