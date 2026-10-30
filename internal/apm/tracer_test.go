package apm

import (
	"context"
	"errors"
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
