package events_test

import (
	"context"
	"errors"
	"testing"

	"transport-app/internal/events"
)

func TestInMemoryBus_PublishSubscribeUnsubscribe(t *testing.T) {
	bus := events.NewInMemoryBus()
	ctx := context.Background()

	calledCount := 0
	unsub := bus.Subscribe("TestEvent", func(ctx context.Context, e events.Event) error {
		calledCount++
		return nil
	})

	bus.Publish(ctx, events.Event{Type: "TestEvent", Payload: "hello"})

	if calledCount != 1 {
		t.Fatalf("expected handler to be called 1 time, got %d", calledCount)
	}

	// Unsubscribe
	unsub()

	bus.Publish(ctx, events.Event{Type: "TestEvent", Payload: "hello again"})

	if calledCount != 1 {
		t.Fatalf("expected handler to not be called after unsubscribe, got %d", calledCount)
	}
}

func TestInMemoryBus_FailingHandlerDoesNotBlockOthers(t *testing.T) {
	bus := events.NewInMemoryBus()
	ctx := context.Background()

	failingCalled := false
	secondCalled := false
	bus.Subscribe("Fanout", func(ctx context.Context, e events.Event) error {
		failingCalled = true
		return errors.New("boom")
	})
	bus.Subscribe("Fanout", func(ctx context.Context, e events.Event) error {
		secondCalled = true
		return nil
	})

	bus.Publish(ctx, events.Event{Type: "Fanout", Payload: "x"})

	if !failingCalled || !secondCalled {
		t.Fatalf("fan-out broken: failing=%v second=%v, want both true", failingCalled, secondCalled)
	}
}
