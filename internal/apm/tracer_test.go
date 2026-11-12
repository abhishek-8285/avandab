package apm

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTracer_StartSpanAndEnd(t *testing.T) {
	tr := NewTracer(100)
	ctx := context.Background()

	_, span := tr.StartSpan(ctx, "test.op")
	if span.TraceID == "" {
		t.Fatal("expected non-empty TraceID")
	}
	if span.SpanID == "" {
		t.Fatal("expected non-empty SpanID")
	}

	span.SetTag("route", "/api/v1/trips")
	span.SetError(errors.New("db error"))

	time.Sleep(10 * time.Millisecond)
	span.End()

	// Double end should be idempotent
	span.End()

	stats := tr.GetStats("test.op")
	if stats.Count != 1 {
		t.Fatalf("expected count 1, got %d", stats.Count)
	}
	if stats.Errors != 1 {
		t.Fatalf("expected errors 1, got %d", stats.Errors)
	}
	if stats.AvgDuration < 10*time.Millisecond {
		t.Fatalf("expected duration >= 10ms, got %v", stats.AvgDuration)
	}
	if stats.P95 < 10*time.Millisecond {
		t.Fatalf("expected P95 >= 10ms, got %v", stats.P95)
	}
}

func TestTracer_ParentChildContext(t *testing.T) {
	tr := NewTracer(50)
	parentCtx, parentSpan := tr.StartSpan(context.Background(), "parent.http")
	defer parentSpan.End()

	childCtx, childSpan := tr.StartSpan(parentCtx, "child.db")
	defer childSpan.End()

	if childSpan.TraceID != parentSpan.TraceID {
		t.Fatalf("child trace ID %s != parent trace ID %s", childSpan.TraceID, parentSpan.TraceID)
	}
	if childSpan.ParentSpanID != parentSpan.SpanID {
		t.Fatalf("child parent span ID %s != parent span ID %s", childSpan.ParentSpanID, parentSpan.SpanID)
	}

	activeFromContext := SpanFromContext(childCtx)
	if activeFromContext != childSpan {
		t.Fatal("SpanFromContext did not return the active child span")
	}
}

func TestTracer_PercentileCalculation(t *testing.T) {
	tr := NewTracer(200)

	// Ingest 100 observations: 1ms, 2ms, ... 100ms
	for i := 1; i <= 100; i++ {
		_, span := tr.StartSpan(context.Background(), "query")
		span.StartTime = time.Now().Add(-time.Duration(i) * time.Millisecond)
		span.End()
	}

	stats := tr.GetStats("query")
	if stats.Count != 100 {
		t.Fatalf("expected count 100, got %d", stats.Count)
	}

	// With 1..100ms, p50 should be approx 50ms, p90 approx 90ms, p95 approx 95ms
	if stats.P50 < 45*time.Millisecond || stats.P50 > 55*time.Millisecond {
		t.Errorf("p50 outside expected range: %v", stats.P50)
	}
	if stats.P95 < 90*time.Millisecond || stats.P95 > 98*time.Millisecond {
		t.Errorf("p95 outside expected range: %v", stats.P95)
	}
	if stats.MaxDuration < 99*time.Millisecond {
		t.Errorf("max duration outside expected range: %v", stats.MaxDuration)
	}
}

func TestTracer_SLABreachAlert(t *testing.T) {
	tr := NewTracer(100)
	var breachFired int32
	var breachOp string
	var breachDur time.Duration

	tr.SetBreachHandler(func(opName string, dur time.Duration, threshold time.Duration, span *Span) {
		atomic.AddInt32(&breachFired, 1)
		breachOp = opName
		breachDur = dur
	})

	tr.SetSLAThreshold("telemetry.ingest", 20*time.Millisecond)

	// Span under threshold: 5ms
	_, s1 := tr.StartSpan(context.Background(), "telemetry.ingest")
	s1.StartTime = time.Now().Add(-5 * time.Millisecond)
	s1.End()

	time.Sleep(10 * time.Millisecond)
	if atomic.LoadInt32(&breachFired) != 0 {
		t.Fatal("breach alert should not fire when duration < threshold")
	}

	// Span exceeding threshold: 30ms
	_, s2 := tr.StartSpan(context.Background(), "telemetry.ingest")
	s2.StartTime = time.Now().Add(-30 * time.Millisecond)
	s2.End()

	// Wait for async callback
	time.Sleep(50 * time.Millisecond)
	if atomic.LoadInt32(&breachFired) != 1 {
		t.Fatalf("expected breach alert to fire once, got %d", atomic.LoadInt32(&breachFired))
	}
	if breachOp != "telemetry.ingest" {
		t.Errorf("expected breach op 'telemetry.ingest', got %s", breachOp)
	}
	if breachDur < 30*time.Millisecond {
		t.Errorf("expected breach duration >= 30ms, got %v", breachDur)
	}
}

func TestTracer_DefaultSLAThresholds(t *testing.T) {
	tr := NewTracer(10)

	httpSLA, ok := tr.SLAThreshold("http.request")
	if !ok || httpSLA != 500*time.Millisecond {
		t.Fatalf("expected default http.request SLA 500ms, got %v (configured=%v)", httpSLA, ok)
	}
	ingestSLA, ok := tr.SLAThreshold("telemetry.batch_ingest")
	if !ok || ingestSLA != 2*time.Second {
		t.Fatalf("expected default telemetry.batch_ingest SLA 2s, got %v (configured=%v)", ingestSLA, ok)
	}

	// Explicit configuration must still win over the default.
	tr.SetSLAThreshold("http.request", 50*time.Millisecond)
	if got, _ := tr.SLAThreshold("http.request"); got != 50*time.Millisecond {
		t.Fatalf("SetSLAThreshold override ignored: got %v", got)
	}
}

func TestTracer_DefaultThresholdFiresBreachCallback(t *testing.T) {
	tr := NewTracer(10)

	type breach struct {
		op        string
		threshold time.Duration
	}
	fired := make(chan breach, 1)
	tr.SetBreachHandler(func(opName string, dur time.Duration, threshold time.Duration, span *Span) {
		select {
		case fired <- breach{op: opName, threshold: threshold}:
		default:
		}
	})

	// No SetSLAThreshold call: the tracer's defaults alone must drive alerting.
	// A span stamped 1s in the past deterministically exceeds the 500ms default.
	_, span := tr.StartSpan(context.Background(), "http.request")
	span.StartTime = time.Now().Add(-time.Second)
	span.End()

	select {
	case b := <-fired:
		if b.op != "http.request" {
			t.Fatalf("expected breach op 'http.request', got %q", b.op)
		}
		if b.threshold != 500*time.Millisecond {
			t.Fatalf("expected breach threshold 500ms, got %v", b.threshold)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("breach callback did not fire: no default SLA threshold configured for http.request")
	}

	// An explicit 1ns override must also fire, carrying the override value.
	tr.SetSLAThreshold("http.request", time.Nanosecond)
	_, s2 := tr.StartSpan(context.Background(), "http.request")
	s2.StartTime = time.Now().Add(-time.Millisecond)
	s2.End()

	select {
	case b := <-fired:
		if b.threshold != time.Nanosecond {
			t.Fatalf("expected breach threshold 1ns, got %v", b.threshold)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("breach callback did not fire for the 1ns override")
	}
}

func TestTracer_ConcurrencySafe(t *testing.T) {
	tr := NewTracer(500)
	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				ctx, span := tr.StartSpan(context.Background(), "concurrent.work")
				if workerID%5 == 0 {
					span.SetError(errors.New("err"))
				}
				span.SetTag("worker", string(rune('A'+workerID)))
				_ = TraceIDFromContext(ctx)
				span.End()
			}
		}(i)
	}

	wg.Wait()

	stats := tr.GetStats("concurrent.work")
	if stats.Count != 1000 {
		t.Fatalf("expected 1000 spans recorded, got %d", stats.Count)
	}
	all := tr.AllStats()
	if len(all) != 1 {
		t.Fatalf("expected 1 operation family, got %d", len(all))
	}
}

// countingLogHandler counts slog records so throttle tests assert on emissions.
type countingLogHandler struct {
	mu sync.Mutex
	n  int
}

func (h *countingLogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *countingLogHandler) Handle(context.Context, slog.Record) error {
	h.mu.Lock()
	h.n++
	h.mu.Unlock()
	return nil
}

func (h *countingLogHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *countingLogHandler) WithGroup(string) slog.Handler      { return h }

func (h *countingLogHandler) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.n
}

// freezeBreachClock captures breach logs and pins the handler clock to start.
// Returns the log counter and a pointer so a test can advance time.
func freezeBreachClock(t *testing.T, start time.Time) (*countingLogHandler, *time.Time) {
	t.Helper()
	h := &countingLogHandler{}
	prevLogger := slog.Default()
	slog.SetDefault(slog.New(h))

	cur := start
	prevNow := now
	now = func() time.Time { return cur }

	breachLog.Lock()
	breachLog.last = map[string]time.Time{}
	breachLog.Unlock()

	t.Cleanup(func() {
		slog.SetDefault(prevLogger)
		now = prevNow
	})
	return h, &cur
}

func TestDefaultBreachHandler_ThrottlesBurstToOnePerWindow(t *testing.T) {
	h, cur := freezeBreachClock(t, time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))

	for i := 0; i < 50; i++ {
		defaultBreachHandler("http.request", 900*time.Millisecond, 500*time.Millisecond, &Span{TraceID: "trace"})
	}
	if got := h.count(); got != 1 {
		t.Fatalf("expected 1 warning for 50 breaches inside the window, got %d", got)
	}

	// Still inside the 1-minute window.
	*cur = cur.Add(59 * time.Second)
	defaultBreachHandler("http.request", 900*time.Millisecond, 500*time.Millisecond, &Span{TraceID: "trace"})
	if got := h.count(); got != 1 {
		t.Fatalf("expected warning suppressed 59s after the first, got %d", got)
	}

	// Advance past the window: one more warning.
	*cur = cur.Add(2 * time.Second)
	defaultBreachHandler("http.request", 900*time.Millisecond, 500*time.Millisecond, &Span{TraceID: "trace"})
	if got := h.count(); got != 2 {
		t.Fatalf("expected a second warning after the window elapsed, got %d", got)
	}
}

func TestDefaultBreachHandler_ThrottlesPerOperationAndThreshold(t *testing.T) {
	h, _ := freezeBreachClock(t, time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))

	defaultBreachHandler("http.request", 900*time.Millisecond, 500*time.Millisecond, nil)
	defaultBreachHandler("telemetry.batch_ingest", 3*time.Second, 2*time.Second, nil)
	if got := h.count(); got != 2 {
		t.Fatalf("expected distinct operations to throttle independently, got %d warnings", got)
	}

	// Same operation, different threshold is a distinct pair.
	defaultBreachHandler("http.request", 2*time.Second, 1500*time.Millisecond, nil)
	if got := h.count(); got != 3 {
		t.Fatalf("expected a distinct threshold to warn independently, got %d warnings", got)
	}
}
