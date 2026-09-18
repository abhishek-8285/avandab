// Package events provides a lightweight event bus for publishing and
// subscribing to domain events. A synchronous in-memory implementation
// is provided; the interface allows swapping in an async broker later.
package events

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
)

// Event is a domain event with a type and payload.
type Event struct {
	// Type uniquely identifies the event kind, e.g. "booking.confirmed".
	Type string
	// Payload carries the event-specific data.
	Payload any
}

// Handler processes an event. Return an error to signal failure.
type Handler func(ctx context.Context, e Event) error

// EventBus allows services to publish events and register handlers.
type EventBus interface {
	Publish(ctx context.Context, e Event) error
	Subscribe(eventType string, h Handler) (unsubscribe func())
}

// InMemoryBus is a synchronous event bus safe for concurrent use.
// Errors returned by handlers are logged (never silently swallowed) and a
// panicking subscriber is recovered so it cannot crash the publisher.
type InMemoryBus struct {
	mu   sync.RWMutex
	subs map[string][]*registeredHandler
	log  *slog.Logger
}

// registeredHandler carries the handler plus a stable identity so unsubscribe
// removes the exact handler even when registrations change order (the old
// index-based removal could delete the wrong handler).
type registeredHandler struct {
	handler Handler
}

// NewInMemoryBus creates a new empty InMemoryBus.
func NewInMemoryBus() *InMemoryBus {
	return &InMemoryBus{
		subs: make(map[string][]*registeredHandler),
		log:  slog.Default(),
	}
}

// Publish synchronously dispatches an event to all registered handlers
// for the event type. Handlers run in the caller's goroutine. A failing
// handler does NOT block its siblings, but its error is aggregated into the
// return value (panics included) so the outbox relay can hold the event
// pending instead of acknowledging a half-delivered fan-out.
func (b *InMemoryBus) Publish(ctx context.Context, e Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.RLock()
	handlers := make([]*registeredHandler, len(b.subs[e.Type]))
	copy(handlers, b.subs[e.Type])
	b.mu.RUnlock()

	var errs []error
	for _, rh := range handlers {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		if err := b.dispatch(ctx, e, rh.handler); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (b *InMemoryBus) dispatch(ctx context.Context, e Event, h Handler) (err error) {
	defer func() {
		if r := recover(); r != nil {
			b.log.Error("event handler panicked",
				"event_type", e.Type,
				"panic", r,
				"stack", string(debug.Stack()))
			err = fmt.Errorf("event handler panicked: %v", r)
		}
	}()

	if err := h(ctx, e); err != nil {
		// Never swallow handler errors: log them so a failing subscriber
		// (e.g. auto-trip-creation) is observable instead of silently dead.
		b.log.Error("event handler failed",
			"event_type", e.Type,
			"error", err)
		return err
	}
	return nil
}

// Subscribe registers a handler for a given event type and returns an
// unsubscribe function. The unsubscribe removes this exact handler by
// identity, safe under concurrent modification.
func (b *InMemoryBus) Subscribe(eventType string, h Handler) (unsubscribe func()) {
	b.mu.Lock()
	defer b.mu.Unlock()

	rh := &registeredHandler{handler: h}
	b.subs[eventType] = append(b.subs[eventType], rh)

	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		subs := b.subs[eventType]
		for i, s := range subs {
			if s == rh {
				b.subs[eventType] = append(subs[:i], subs[i+1:]...)
				return
			}
		}
	}
}
