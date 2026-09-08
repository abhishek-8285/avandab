package realtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	tripevents "transport-app/internal/domain/trip"
	"transport-app/internal/events"
	"transport-app/internal/shared"
)

type flushRecorder struct {
	*httptest.ResponseRecorder
	flushed bool
	mu      sync.Mutex
	writes  chan string
}

func newFlushRecorder() *flushRecorder {
	return &flushRecorder{
		ResponseRecorder: httptest.NewRecorder(),
		writes:           make(chan string, 100),
	}
}

func (f *flushRecorder) Flush() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.flushed = true
}

func (f *flushRecorder) Write(b []byte) (int, error) {
	f.mu.Lock()
	f.writes <- string(b)
	f.mu.Unlock()
	return f.ResponseRecorder.Write(b)
}

func TestStreamHandler_SSEDisabled(t *testing.T) {
	hub := NewHub(15, nil)
	handler := StreamHandler(hub, false)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/telemetry/stream", nil)
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503 Service Unavailable, got %d", rec.Code)
	}
}

func TestStreamHandler_HeadersAndStream(t *testing.T) {
	hub := NewHub(15, nil)
	handler := StreamHandler(hub, true)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/telemetry/stream", nil).WithContext(ctx)
	rec := newFlushRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		handler(rec, req)
	}()

	// Wait a moment for handler to subscribe and flush headers
	time.Sleep(20 * time.Millisecond)

	if rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Errorf("expected Content-Type text/event-stream, got %q", rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("expected Cache-Control no-cache, got %q", rec.Header().Get("Cache-Control"))
	}
	if rec.Header().Get("Connection") != "keep-alive" {
		t.Errorf("expected Connection keep-alive, got %q", rec.Header().Get("Connection"))
	}
	if rec.Header().Get("X-Accel-Buffering") != "no" {
		t.Errorf("expected X-Accel-Buffering no, got %q", rec.Header().Get("X-Accel-Buffering"))
	}

	// Publish event to hub
	hub.Publish(context.Background(), events.Event{
		Type: "telemetry.snapshot",
		Payload: map[string]interface{}{
			"vehicle_id": "v-stream-1",
			"speed":      60.5,
		},
	})

	select {
	case frame := <-rec.writes:
		if !strings.HasPrefix(frame, "event: telemetry\ndata: ") {
			t.Fatalf("unexpected frame format: %q", frame)
		}
		if !strings.Contains(frame, "v-stream-1") {
			t.Fatalf("frame does not contain vehicle id: %q", frame)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for streamed event")
	}

	// Cancel context to cleanly shut down handler
	cancel()
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("handler did not exit on client context cancellation")
	}
}

func TestStreamHandler_QueryFilters(t *testing.T) {
	hub := NewHub(15, nil)
	handler := StreamHandler(hub, true)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/telemetry/stream?trip_id=trip-alpha&vehicle_id=veh-alpha", nil).WithContext(ctx)
	rec := newFlushRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		handler(rec, req)
	}()

	time.Sleep(20 * time.Millisecond)

	// Publish non-matching event (different vehicle)
	hub.Publish(context.Background(), events.Event{
		Type: "telemetry.snapshot",
		Payload: map[string]interface{}{
			"trip_id":    "trip-alpha",
			"vehicle_id": "veh-beta",
		},
	})

	// Publish non-matching event (different trip)
	hub.Publish(context.Background(), events.Event{
		Type: "telemetry.snapshot",
		Payload: map[string]interface{}{
			"trip_id":    "trip-beta",
			"vehicle_id": "veh-alpha",
		},
	})

	select {
	case frame := <-rec.writes:
		t.Fatalf("unexpected frame received for non-matching events: %s", frame)
	case <-time.After(50 * time.Millisecond):
		// Expected timeout
	}

	// Publish matching event
	hub.Publish(context.Background(), events.Event{
		Type: "telemetry.snapshot",
		Payload: map[string]interface{}{
			"trip_id":    "trip-alpha",
			"vehicle_id": "veh-alpha",
			"speed":      55.0,
		},
	})

	select {
	case frame := <-rec.writes:
		if !strings.Contains(frame, "trip-alpha") || !strings.Contains(frame, "veh-alpha") {
			t.Fatalf("expected frame matching filter, got %s", frame)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for matching event")
	}

	cancel()
	<-done
}

func TestStreamHandler_TenantIsolation(t *testing.T) {
	hub := NewHub(15, nil)
	handler := StreamHandler(hub, true)

	ctx, cancel := context.WithCancel(shared.ContextWithTenantID(context.Background(), "1"))
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/telemetry/stream", nil).WithContext(ctx)
	rec := newFlushRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		handler(rec, req)
	}()
	time.Sleep(20 * time.Millisecond)

	// Cross-tenant stamped event must never reach this stream.
	hub.Publish(context.Background(), events.Event{
		Type:    "telemetry.snapshot",
		Payload: map[string]interface{}{"tenant_id": "2", "vehicle_id": "v-other"},
	})
	// Same-tenant stamped event must arrive.
	hub.Publish(context.Background(), events.Event{
		Type:    "telemetry.snapshot",
		Payload: map[string]interface{}{"tenant_id": "1", "vehicle_id": "v-mine"},
	})

	select {
	case frame := <-rec.writes:
		if !strings.Contains(frame, "v-mine") {
			t.Fatalf("expected same-tenant frame, got %s", frame)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for same-tenant event")
	}
	select {
	case frame := <-rec.writes:
		t.Fatalf("cross-tenant event leaked: %s", frame)
	case <-time.After(100 * time.Millisecond):
	}

	// Struct payloads (SOS) carry tenant_id via json tags — same rule.
	hub.Publish(context.Background(), events.Event{
		Type:    "SOSEvent",
		Payload: map[string]interface{}{"tenant_id": "2", "vehicle_id": "v-sos-other"},
	})
	select {
	case frame := <-rec.writes:
		t.Fatalf("cross-tenant SOS leaked: %s", frame)
	case <-time.After(100 * time.Millisecond):
	}

	cancel()
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("handler did not exit on client context cancellation")
	}
}

func TestStreamHandler_TenantIsolationStructPayload(t *testing.T) {
	hub := NewHub(15, nil)
	handler := StreamHandler(hub, true)

	ctx, cancel := context.WithCancel(shared.ContextWithTenantID(context.Background(), "1"))
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/telemetry/stream", nil).WithContext(ctx)
	rec := newFlushRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		handler(rec, req)
	}()
	time.Sleep(20 * time.Millisecond)

	// Struct payloads marshal TenantID (capital key) — same rule applies.
	hub.Publish(context.Background(), events.Event{
		Type:    "trip.completed",
		Payload: tripevents.TripCompletedEvent{TripID: "t-1", TenantID: "2"},
	})
	hub.Publish(context.Background(), events.Event{
		Type:    "trip.completed",
		Payload: tripevents.TripCompletedEvent{TripID: "t-2", TenantID: "1"},
	})

	select {
	case frame := <-rec.writes:
		if !strings.Contains(frame, "t-2") {
			t.Fatalf("expected same-tenant trip frame, got %s", frame)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for same-tenant trip event")
	}
	select {
	case frame := <-rec.writes:
		t.Fatalf("cross-tenant trip event leaked: %s", frame)
	case <-time.After(100 * time.Millisecond):
	}

	cancel()
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("handler did not exit on client context cancellation")
	}
}
