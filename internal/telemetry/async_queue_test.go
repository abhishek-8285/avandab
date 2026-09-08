package telemetry

import (
	"context"
	"testing"
	"time"

	"transport-app/internal/telemetry/providers"
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

// Saturated queue must NOT lose hardware-acked frames: IngestAsync falls back
// to the synchronous pipeline (backpressure) instead of dropping.
func TestIngestAsync_SaturatedQueuePersists(t *testing.T) {
	db := newTestIngestorDB(t)
	insertTestVehicle(t, db, "v-1")
	insertTestDevice(t, db, "IMEI-SAT", DeviceStatusActive, strPtr("v-1"))
	ing := newTestIngestor(t, db, nil)

	q := NewAsyncIngestQueue(1, 0, nil, nil) // unstarted: nobody drains
	ing.SetQueue(q)
	if !q.Push(RawFrame{IMEI: "FILLER"}) {
		t.Fatal("setup: filler push must succeed")
	}

	frame := providers.RawFrame{
		IMEI:          "IMEI-SAT",
		DeviceTime:    time.Now().UTC().Add(-2 * time.Second),
		Latitude:      19.07,
		Longitude:     72.83,
		Speed:         45.0,
		Provider:      "own",
		ProviderMsgID: "msg-sat-001",
	}
	if err := ing.IngestAsync(context.Background(), frame); err != nil {
		t.Fatalf("IngestAsync on saturated queue = %v, want nil (sync fallback)", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM telemetry_positions WHERE imei = 'IMEI-SAT'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("positions for IMEI-SAT = %d, want 1 (frame lost despite ACK)", n)
	}
}
