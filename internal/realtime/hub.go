// Package realtime provides a single-process, in-memory SSE fan-out hub for
// live telemetry on the tracking map (Spec 04 §1.2).
//
// The hub is intentionally process-local: a single-binary deployment sees every
// snapshot event; multi-instance deployments fall back to REST polling
// (GET /api/v1/telemetry/live), which is the source of truth (§11.4).
package realtime

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"transport-app/internal/events"
)

// defaultBufferCap is the per-subscriber channel buffer. A slow consumer whose
// buffer fills is dropped (the browser's EventSource auto-reconnects and
// re-polls /live to catch missed events). See Spec 04 §11.4.
const defaultBufferCap = 64

// filter returns true when the event should be delivered to a subscriber.
// A nil filter accepts everything.
type filter func(e events.Event) bool

// Filter is the exported alias so external Broadcaster implementations (a
// future Redis/NATS fan-out) can name the predicate type.
type Filter = filter

// Broadcaster is the fan-out seam: Hub is the single-process implementation.
// A future Redis/NATS implementation satisfies this interface without
// touching AttachToBus, StreamHandler, or any call site — multi-instance
// fan-out then becomes a wiring change in main.go, not a refactor.
type Broadcaster interface {
	Subscribe(ctx context.Context, f Filter) (<-chan []byte, func())
	Publish(ctx context.Context, e events.Event)
	Run(ctx context.Context)
}

var _ Broadcaster = (*Hub)(nil)

// Hub broadcasts published events to SSE subscribers. It is safe for
// concurrent use.
type Hub struct {
	mu        sync.RWMutex
	subs      map[chan []byte]filter
	keepalive time.Duration
	logger    *slog.Logger
}

// NewHub creates a Hub that emits a keepalive frame every keepaliveSec
// (default 15s when <= 0).
func NewHub(keepaliveSec int, logger *slog.Logger) *Hub {
	if keepaliveSec <= 0 {
		keepaliveSec = 15
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Hub{
		subs:      make(map[chan []byte]filter),
		keepalive: time.Duration(keepaliveSec) * time.Second,
		logger:    logger,
	}
}

// Subscribe returns a channel of SSE-formatted byte frames and an unsubscribe
// function. The channel is buffered with capacity defaultBufferCap. The
// caller MUST drain the channel (or unsubscribe) or the subscription is
// treated as a slow consumer and dropped on the next Publish.
func (h *Hub) Subscribe(_ context.Context, f filter) (<-chan []byte, func()) {
	ch := make(chan []byte, defaultBufferCap)
	h.mu.Lock()
	h.subs[ch] = f
	h.mu.Unlock()

	unsub := func() {
		h.mu.Lock()
		if _, ok := h.subs[ch]; ok {
			delete(h.subs, ch)
			close(ch)
		}
		h.mu.Unlock()
	}
	return ch, unsub
}

// Publish fans out an event to all matching subscribers. Non-blocking:
// if a subscriber's channel is full, it is dropped (slow consumer protection:
// channel is closed and removed from hub; client EventSource auto-reconnects
// and re-polls /live). Called from the bus handler; must never block ingestion.
func (h *Hub) Publish(_ context.Context, e events.Event) {
	payload, err := json.Marshal(e.Payload)
	if err != nil {
		return
	}
	// Spec 04 §1.2: SSE frame for telemetry payloads. The event name is fixed
	// at "telemetry" so the client listens via es.addEventListener('telemetry').
	frame := []byte("event: telemetry\ndata: " + string(payload) + "\n\n")

	var slowChans []chan []byte

	h.mu.RLock()
	for ch, f := range h.subs {
		if f != nil && !f(e) {
			continue
		}
		select {
		case ch <- frame:
		default:
			slowChans = append(slowChans, ch)
		}
	}
	h.mu.RUnlock()

	if len(slowChans) > 0 {
		h.mu.Lock()
		for _, ch := range slowChans {
			if _, ok := h.subs[ch]; ok {
				delete(h.subs, ch)
				close(ch)
				h.logger.Warn("dropped slow SSE subscriber")
			}
		}
		h.mu.Unlock()
	}
}

// Run starts the keepalive heartbeat. Each tick writes a SSE comment frame
// (": keepalive\n\n") — ignored by clients but keeps proxies/firewalls from
// closing idle connections. Exits when ctx is cancelled; subscriber channels
// are closed by the handler's defer unsub() on client disconnect.
func (h *Hub) Run(ctx context.Context) {
	ticker := time.NewTicker(h.keepalive)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.mu.RLock()
			for ch := range h.subs {
				select {
				case ch <- []byte(": keepalive\n\n"):
				default:
				}
			}
			h.mu.RUnlock()
		}
	}
}
