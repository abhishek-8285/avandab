package deviation

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"transport-app/internal/events"
)

// failingBus is an EventBus whose Publish always fails. It verifies that a
// caller-side publish failure is logged instead of silently discarded.
type failingBus struct{ err error }

func (b failingBus) Publish(_ context.Context, _ events.Event) error { return b.err }
func (b failingBus) Subscribe(_ string, _ events.Handler) func()      { return func() {} }

func TestSafePublishLogsFailure(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	e := NewEngine(nil, failingBus{err: errors.New("broker down")}, nil, logger)

	e.safePublish(context.Background(), events.Event{Type: events.GPSDeviationAlert})

	got := buf.String()
	if !strings.Contains(got, "publish failed") || !strings.Contains(got, "broker down") {
		t.Fatalf("expected the publish failure to be logged, got %q", got)
	}
}

func TestSafePublishNoBusIsNoop(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	e := NewEngine(nil, nil, nil, logger)

	e.safePublish(context.Background(), events.Event{Type: "anything"})

	if buf.Len() != 0 {
		t.Fatalf("a nil bus should be a silent no-op, got %q", buf.String())
	}
}
