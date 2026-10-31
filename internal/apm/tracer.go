package apm

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"math/big"
	"sort"
	"sync"
	"time"
)

// ContextKey is a type for APM context keys.
type ContextKey string

const (
	// TraceIDKey is the context key for the distributed trace ID.
	TraceIDKey ContextKey = "apm_trace_id"
	// SpanIDKey is the context key for the active span ID.
	SpanIDKey ContextKey = "apm_span_id"
	// SpanKey is the context key for the current *Span object.
	SpanKey ContextKey = "apm_active_span"
)

// Span represents a single timed unit of work.
type Span struct {
	TraceID      string            `json:"trace_id"`
	SpanID       string            `json:"span_id"`
	ParentSpanID string            `json:"parent_span_id,omitempty"`
	Name         string            `json:"name"`
	StartTime    time.Time         `json:"start_time"`
	Duration     time.Duration     `json:"duration"`
	Tags         map[string]string `json:"tags,omitempty"`
	Err          error             `json:"-"`

	tracer *Tracer
	mu     sync.Mutex
	ended  bool
}

// SetTag adds or updates a key-value tag on the span.
func (s *Span) SetTag(key, value string) *Span {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Tags == nil {
		s.Tags = make(map[string]string)
	}
	s.Tags[key] = value
	return s
}

// SetError records an error onto the span.
func (s *Span) SetError(err error) *Span {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Err = err
	if err != nil {
		if s.Tags == nil {
			s.Tags = make(map[string]string)
		}
		s.Tags["error"] = "true"
		s.Tags["error.message"] = err.Error()
	}
	return s
}

// End completes the span, calculating its duration and publishing to the tracer.
func (s *Span) End() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.ended {
		s.mu.Unlock()
		return
	}
	s.ended = true
	s.Duration = time.Since(s.StartTime)
	s.mu.Unlock()

	if s.tracer != nil {
		s.tracer.recordSpan(s)
	}
}

// OperationStats captures aggregated latency and throughput metrics for an operation.
type OperationStats struct {
	Name        string        `json:"name"`
	Count       int64         `json:"count"`
	Errors      int64         `json:"errors"`
	P50         time.Duration `json:"p50"`
	P90         time.Duration `json:"p90"`
	P95         time.Duration `json:"p95"`
	P99         time.Duration `json:"p99"`
	AvgDuration time.Duration `json:"avg_duration"`
	MaxDuration time.Duration `json:"max_duration"`
}

// BreachHandler is invoked when an operation's latency exceeds its P95 SLA threshold.
type BreachHandler func(opName string, dur time.Duration, threshold time.Duration, span *Span)

// Tracer accumulates trace spans and maintains rolling latency percentiles.
type Tracer struct {
	mu          sync.RWMutex
	samples     map[string][]time.Duration
	sampleLimit int
	counts      map[string]int64
	errors      map[string]int64
	durSums     map[string]time.Duration
	maxDurs     map[string]time.Duration
	thresholds  map[string]time.Duration
	onBreach    BreachHandler
}

// Default p95 SLA thresholds for the operations instrumented in production.
const (
	defaultHTTPRequestSLA time.Duration = 500 * time.Millisecond
	defaultIngestBatchSLA time.Duration = 2 * time.Second
)

// defaultSLAThresholds returns the production defaults; SetSLAThreshold overrides them.
func defaultSLAThresholds() map[string]time.Duration {
	return map[string]time.Duration{
		"http.request":           defaultHTTPRequestSLA,
		"telemetry.batch_ingest": defaultIngestBatchSLA,
	}
}

// NewTracer constructs an APM tracer with an in-memory reservoir sample size.
func NewTracer(sampleLimit int) *Tracer {
	if sampleLimit <= 0 {
		sampleLimit = 1000
	}
	return &Tracer{
		samples:     make(map[string][]time.Duration),
		sampleLimit: sampleLimit,
		counts:      make(map[string]int64),
		errors:      make(map[string]int64),
		durSums:     make(map[string]time.Duration),
		maxDurs:     make(map[string]time.Duration),
		thresholds:  defaultSLAThresholds(),
		onBreach:    defaultBreachHandler,
	}
}

// Default is the package-level tracer instance used application-wide.
var Default = NewTracer(1000)

func defaultBreachHandler(opName string, dur time.Duration, threshold time.Duration, span *Span) {
	traceID := ""
	if span != nil {
		traceID = span.TraceID
	}
	slog.Warn("APM SLA latency breach detected",
		slog.String("operation", opName),
		slog.Duration("duration", dur),
		slog.Duration("p95_sla_threshold", threshold),
		slog.String("trace_id", traceID),
	)
}

// SetSLAThreshold configures a p95 SLA duration threshold for an operation.
func (t *Tracer) SetSLAThreshold(opName string, threshold time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.thresholds[opName] = threshold
}

// SLAThreshold reports the p95 threshold configured for an operation.
func (t *Tracer) SLAThreshold(opName string) (time.Duration, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	threshold, ok := t.thresholds[opName]
	return threshold, ok
}

// SetBreachHandler sets the callback for SLA breach alerts.
func (t *Tracer) SetBreachHandler(handler BreachHandler) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if handler == nil {
		handler = defaultBreachHandler
	}
	t.onBreach = handler
}

// StartSpan creates a new Span, deriving or creating a TraceID and parent linkage.
func (t *Tracer) StartSpan(ctx context.Context, name string) (context.Context, *Span) {
	if ctx == nil {
		ctx = context.Background()
	}

	traceID := TraceIDFromContext(ctx)
	if traceID == "" {
		traceID = GenerateID(16) // 128-bit W3C compliant hex ID
	}

	parentSpanID := ""
	if parent := SpanFromContext(ctx); parent != nil {
		parentSpanID = parent.SpanID
	} else if pID, ok := ctx.Value(SpanIDKey).(string); ok {
		parentSpanID = pID
	}

	spanID := GenerateID(8) // 64-bit span ID

	span := &Span{
		TraceID:      traceID,
		SpanID:       spanID,
		ParentSpanID: parentSpanID,
		Name:         name,
		StartTime:    time.Now(),
		Tags:         make(map[string]string),
		tracer:       t,
	}

	childCtx := context.WithValue(ctx, TraceIDKey, traceID)
	childCtx = context.WithValue(childCtx, SpanIDKey, spanID)
	childCtx = context.WithValue(childCtx, SpanKey, span)
	childCtx = context.WithValue(childCtx, "trace_id", traceID)

	return childCtx, span
}

// StartSpan helper using Default tracer.
func StartSpan(ctx context.Context, name string) (context.Context, *Span) {
	return Default.StartSpan(ctx, name)
}

func (t *Tracer) recordSpan(s *Span) {
	t.mu.Lock()
	defer t.mu.Unlock()

	opName := s.Name
	t.counts[opName]++
	t.durSums[opName] += s.Duration
	if s.Duration > t.maxDurs[opName] {
		t.maxDurs[opName] = s.Duration
	}
	if s.Err != nil {
		t.errors[opName]++
	}

	// Reservoir sampling (Algorithm R)
	currentSamples := t.samples[opName]
	n := t.counts[opName]
	if len(currentSamples) < t.sampleLimit {
		t.samples[opName] = append(currentSamples, s.Duration)
	} else {
		// Random index between 0 and n-1
		idxBig, err := rand.Int(rand.Reader, big.NewInt(n))
		if err == nil {
			idx := idxBig.Int64()
			if idx < int64(t.sampleLimit) {
				currentSamples[idx] = s.Duration
			}
		}
	}

	// Check SLA breach
	threshold, hasThreshold := t.thresholds[opName]
	if hasThreshold && s.Duration > threshold && t.onBreach != nil {
		// Fire callback asynchronously to avoid blocking tracer lock
		go t.onBreach(opName, s.Duration, threshold, s)
	}
}

// GetStats returns aggregated OperationStats for a named operation.
func (t *Tracer) GetStats(name string) OperationStats {
	t.mu.RLock()
	defer t.mu.RUnlock()

	count := t.counts[name]
	if count == 0 {
		return OperationStats{Name: name}
	}

	errCount := t.errors[name]
	avg := time.Duration(int64(t.durSums[name]) / count)
	max := t.maxDurs[name]

	raw := t.samples[name]
	sorted := make([]time.Duration, len(raw))
	copy(sorted, raw)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	return OperationStats{
		Name:        name,
		Count:       count,
		Errors:      errCount,
		P50:         percentile(sorted, 0.50),
		P90:         percentile(sorted, 0.90),
		P95:         percentile(sorted, 0.95),
		P99:         percentile(sorted, 0.99),
		AvgDuration: avg,
		MaxDuration: max,
	}
}

// AllStats returns stats for every operation observed.
func (t *Tracer) AllStats() map[string]OperationStats {
	t.mu.RLock()
	names := make([]string, 0, len(t.counts))
	for name := range t.counts {
		names = append(names, name)
	}
	t.mu.RUnlock()

	result := make(map[string]OperationStats, len(names))
	for _, name := range names {
		result[name] = t.GetStats(name)
	}
	return result
}

// Reset clears all counters and samples (used in tests or periodic resets).
func (t *Tracer) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.samples = make(map[string][]time.Duration)
	t.counts = make(map[string]int64)
	t.errors = make(map[string]int64)
	t.durSums = make(map[string]time.Duration)
	t.maxDurs = make(map[string]time.Duration)
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 1 {
		return sorted[n-1]
	}
	idx := int(float64(n-1) * p)
	return sorted[idx]
}

// TraceIDFromContext extracts a trace ID from known context keys.
func TraceIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	keys := []any{TraceIDKey, "trace_id", "request_id"}
	for _, k := range keys {
		if val := ctx.Value(k); val != nil {
			if s, ok := val.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

// SpanFromContext retrieves the currently active *Span from context.
func SpanFromContext(ctx context.Context) *Span {
	if ctx == nil {
		return nil
	}
	if s, ok := ctx.Value(SpanKey).(*Span); ok {
		return s
	}
	return nil
}

// GenerateID produces a cryptographically random hex string of byteLen bytes.
func GenerateID(byteLen int) string {
	b := make([]byte, byteLen)
	if _, err := rand.Read(b); err != nil {
		// Fallback to nanosecond timestamp if system entropy fails
		return fmt.Sprintf("%016x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
