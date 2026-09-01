package telemetry

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// AsyncIngestQueue is a high-throughput in-memory ring-buffer that decouples
// incoming GPS network connections (HTTP/TCP/MQTT) from database write I/O.
type AsyncIngestQueue struct {
	queue        chan RawFrame
	ingestor     *Ingestor
	logger       *slog.Logger
	workers      int
	wg           sync.WaitGroup
	quit         chan struct{}
	closed       atomic.Bool
	droppedCount atomic.Uint64
}

// NewAsyncIngestQueue constructs an AsyncIngestQueue with the specified capacity.
func NewAsyncIngestQueue(capacity int, workers int, ingestor *Ingestor, logger *slog.Logger) *AsyncIngestQueue {
	if capacity <= 0 {
		capacity = 10000
	}
	if workers <= 0 {
		workers = 4
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &AsyncIngestQueue{
		queue:    make(chan RawFrame, capacity),
		ingestor: ingestor,
		logger:   logger,
		workers:  workers,
		quit:     make(chan struct{}),
	}
}

// Push enqueues a RawFrame in a non-blocking manner (< 0.1ms).
// Returns true if successfully enqueued, false if queue is full or closed.
func (q *AsyncIngestQueue) Push(frame RawFrame) bool {
	if q.closed.Load() {
		return false
	}

	select {
	case q.queue <- frame:
		return true
	default:
		// Queue saturated — record metric and reject without blocking
		q.droppedCount.Add(1)
		q.logger.Warn("telemetry async buffer full, frame dropped",
			"imei", frame.IMEI,
			"dropped_total", q.droppedCount.Load())
		return false
	}
}

// Start launches the background worker pool to process queued frames.
func (q *AsyncIngestQueue) Start(ctx context.Context) {
	for i := 0; i < q.workers; i++ {
		q.wg.Add(1)
		go func(workerID int) {
			defer q.wg.Done()
			q.workerLoop(ctx, workerID)
		}(i)
	}
	q.logger.Info("telemetry async queue workers started", "workers", q.workers, "capacity", cap(q.queue))
}

// workerLoop continuously processes frames from the queue.
func (q *AsyncIngestQueue) workerLoop(ctx context.Context, id int) {
	for {
		select {
		case <-q.quit:
			return
		case <-ctx.Done():
			return
		case frame, ok := <-q.queue:
			if !ok {
				return
			}
			if q.ingestor != nil {
				_, err := q.ingestor.IngestRawFrame(ctx, frame)
				if err != nil && !errors.Is(err, context.Canceled) {
					q.logger.Debug("async ingest frame processing failed", "imei", frame.IMEI, "error", err)
				}
			}
		}
	}
}

// Drain stops accepting new frames and flushes all remaining items in the buffer.
func (q *AsyncIngestQueue) Drain(timeout time.Duration) {
	if q.closed.Swap(true) {
		return // Already draining
	}

	// Drain remaining items up to timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	done := make(chan struct{})
	go func() {
		for len(q.queue) > 0 {
			frame := <-q.queue
			if q.ingestor != nil {
				_, _ = q.ingestor.IngestRawFrame(ctx, frame)
			}
		}
		close(done)
	}()

	select {
	case <-done:
		q.logger.Info("telemetry async queue drained cleanly")
	case <-ctx.Done():
		q.logger.Warn("telemetry async queue drain timed out", "remaining", len(q.queue))
	}

	close(q.quit)
	q.wg.Wait()
}

// Len returns the current count of queued frames in the buffer.
func (q *AsyncIngestQueue) Len() int {
	return len(q.queue)
}

// Cap returns the total buffer capacity.
func (q *AsyncIngestQueue) Cap() int {
	return cap(q.queue)
}
