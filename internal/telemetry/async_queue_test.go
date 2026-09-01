package telemetry

import (
	"context"
	"testing"
	"time"
)

func TestAsyncIngestQueue_PushAndDrain(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	queue := NewAsyncIngestQueue(100, 2, nil, nil)
	queue.Start(ctx)

	// Push 10 frames
	for i := 0; i < 10; i++ {
		pushed := queue.Push(RawFrame{
			IMEI:       "864209048123456",
			Latitude:   19.0760,
			Longitude:  72.8777,
			Speed:      45.0,
			DeviceTime: time.Now().UTC(),
		})
		if !pushed {
			t.Errorf("expected frame %d to be pushed", i)
		}
	}

	if queue.Cap() != 100 {
		t.Errorf("expected capacity 100, got %d", queue.Cap())
	}

	// Drain queue cleanly
	queue.Drain(2 * time.Second)

	if !queue.closed.Load() {
		t.Errorf("expected queue to be marked closed")
	}

	// Post-drain push must return false
	if queue.Push(RawFrame{IMEI: "123"}) {
		t.Errorf("expected push on drained queue to return false")
	}
}

func TestAsyncIngestQueue_SaturationDrop(t *testing.T) {
	// Create queue with capacity 2 and 0 workers to test backpressure
	queue := NewAsyncIngestQueue(2, 0, nil, nil)

	if !queue.Push(RawFrame{IMEI: "1"}) {
		t.Errorf("expected push 1 to succeed")
	}
	if !queue.Push(RawFrame{IMEI: "2"}) {
		t.Errorf("expected push 2 to succeed")
	}

	// 3rd push must safely drop without blocking
	if queue.Push(RawFrame{IMEI: "3"}) {
		t.Errorf("expected 3rd push to be dropped")
	}

	if queue.droppedCount.Load() != 1 {
		t.Errorf("expected dropped count 1, got %d", queue.droppedCount.Load())
	}
}
