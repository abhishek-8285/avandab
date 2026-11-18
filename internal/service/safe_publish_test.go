package service

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"transport-app/internal/events"
)

// failingPublishBus is an EventBus whose Publish always fails. It verifies
// that services log a delivery failure instead of discarding it: the business
// write already committed, so a lost event means a downstream alert or
// automation silently never fires.
type failingPublishBus struct{ err error }

func (b failingPublishBus) Publish(_ context.Context, _ events.Event) error { return b.err }
func (b failingPublishBus) Subscribe(_ string, _ events.Handler) func()     { return func() {} }

func TestBaseServiceSafePublishLogsFailure(t *testing.T) {
	var buf bytes.Buffer
	svc := &baseService{
		log:    slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})),
		events: failingPublishBus{err: errors.New("broker down")},
	}

	svc.safePublish(context.Background(), events.Event{Type: events.TripCreated})

	got := buf.String()
	if !strings.Contains(got, "event publish failed") || !strings.Contains(got, "broker down") {
		t.Fatalf("expected the publish failure to be logged, got %q", got)
	}
	if !strings.Contains(got, events.TripCreated) {
		t.Fatalf("expected the event type to be logged, got %q", got)
	}
}

func TestBaseServiceSafePublishNilBusIsNoop(t *testing.T) {
	var buf bytes.Buffer
	svc := &baseService{
		log: slog.New(slog.NewTextHandler(&buf, nil)),
	}

	svc.safePublish(context.Background(), events.Event{Type: "anything"})

	if buf.Len() != 0 {
		t.Fatalf("a nil bus should be a silent no-op, got %q", buf.String())
	}
}

func TestBaseServiceSafePublishNilLoggerFallsBack(t *testing.T) {
	// A nil logger must not panic on a failing publish; it falls back to the
	// process default logger.
	svc := &baseService{events: failingPublishBus{err: errors.New("bus closed")}}

	svc.safePublish(context.Background(), events.Event{Type: "Probe"})
}
