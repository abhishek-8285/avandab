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
	failedCount  atomic.Uint64
	mu           sync.Mutex
	cancel       context.CancelFunc
}

// FailedCount reports frames the workers accepted from the queue but could
// not persist (W2 visibility: saturation drops are IngestAsync's domain and
// now fall back to sync persist; worker failures are the remaining loss
// vector and must be countable in monitoring).
func (q *AsyncIngestQueue) FailedCount() uint64 { return q.failedCount.Load() }

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
	wctx, cancel := context.WithCancel(ctx)
	q.mu.Lock()
	q.cancel = cancel
	q.mu.Unlock()
	for i := 0; i < q.workers; i++ {
		q.wg.Add(1)
		go func(workerID int) {
			defer q.wg.Done()
			q.workerLoop(wctx, workerID)
		}(i)
	}
	q.logger.Info("telemetry async queue workers started", "workers", q.workers, "capacity", cap(q.queue))
}

// workerLoop continuously processes frames from the queue in batches.
func (q *AsyncIngestQueue) workerLoop(ctx context.Context, id int) {
	batchSize := 100
	flushInterval := 50 * time.Millisecond
	if q.ingestor != nil {
		if q.ingestor.cfg.BatchSize > 0 {
			batchSize = q.ingestor.cfg.BatchSize
		}
		if q.ingestor.cfg.FlushInterval > 0 {
			flushInterval = q.ingestor.cfg.FlushInterval
		}
	}

	batch := make([]RawFrame, 0, batchSize)
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		toFlush := batch
		batch = make([]RawFrame, 0, batchSize)
		if q.ingestor != nil {
			_, err := q.ingestor.IngestBatch(ctx, toFlush)
			if err != nil && !errors.Is(err, context.Canceled) {
				failed := q.failedCount.Add(uint64(len(toFlush)))
				q.logger.Warn("async ingest batch processing failed",
					"count", len(toFlush), "error", err, "failed_total", failed)
			}
		}
	}

	for {
		select {
		case <-q.quit:
			flush()
			return
		case <-ctx.Done():
			flush()
			return
		case <-ticker.C:
			flush()
		case frame, ok := <-q.queue:
			if !ok {
				flush()
				return
			}
			batch = append(batch, frame)
			for len(batch) < batchSize {
				select {
				case next, ok := <-q.queue:
					if !ok {
						flush()
						return
					}
					batch = append(batch, next)
				default:
					goto batchReady
				}
			}
		batchReady:
			if len(batch) >= batchSize {
				flush()
			}
		}
	}
}

// Drain stops accepting new frames and flushes all remaining items in the buffer.
// Wrapper around DrainContext with its own timeout; server shutdown prefers
// DrainContext on the shared shutdown ctx so drain + HTTP shutdown share one
// deadline (see ShutdownSequence).
func (q *AsyncIngestQueue) Drain(timeout time.Duration) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	q.DrainContext(ctx)
}

// DrainContext stops accepting new frames and flushes the remainder under
// the caller's ctx (shared shutdown deadline in prod).
// Single ownership: workers are signalled to stop pulling first, then the
// caller drains the remainder with a deadline ctx. wg.Wait is deadline-bounded
// (stuck workers are cancelled); drain ingest errors are counted + logged.
func (q *AsyncIngestQueue) DrainContext(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	if q.closed.Swap(true) {
		return // Already draining
	}

	q.mu.Lock()
	workerCancel := q.cancel
	q.mu.Unlock()

	close(q.quit)

	waitDone := make(chan struct{})
	go func() {
		q.wg.Wait()
		close(waitDone)
	}()
	workersStuck := false
	select {
	case <-waitDone:
		if workerCancel != nil {
			workerCancel()
		}
	case <-ctx.Done():
		workersStuck = true
		if workerCancel != nil {
			workerCancel()
		}
		q.logger.Warn("telemetry async queue workers did not stop before shutdown deadline")
	}

	batchSize := 100
	if q.ingestor != nil && q.ingestor.cfg.BatchSize > 0 {
		batchSize = q.ingestor.cfg.BatchSize
	}
	drainBatch := make([]RawFrame, 0, batchSize)
	flushDrain := func() {
		if len(drainBatch) == 0 {
			return
		}
		if q.ingestor != nil {
			if _, err := q.ingestor.IngestBatch(ctx, drainBatch); err != nil &&
				!errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				failed := q.failedCount.Add(uint64(len(drainBatch)))
				q.logger.Warn("async ingest drain batch failed",
					"count", len(drainBatch), "error", err, "failed_total", failed)
			}
		}
		drainBatch = make([]RawFrame, 0, batchSize)
	}

	for {
		select {
		case <-ctx.Done():
			flushDrain()
			q.logger.Warn("telemetry async queue drain timed out", "remaining", len(q.queue))
			return
		default:
		}
		select {
		case frame := <-q.queue:
			drainBatch = append(drainBatch, frame)
			if len(drainBatch) >= batchSize {
				flushDrain()
			}
		default:
			flushDrain()
			if workersStuck {
				q.logger.Warn("telemetry async queue drain incomplete, workers stuck", "remaining", len(q.queue))
			} else {
				q.logger.Info("telemetry async queue drained cleanly")
			}
			return
		}
	}
}

// Len returns the current count of queued frames in the buffer.
func (q *AsyncIngestQueue) Len() int {
	return len(q.queue)
}

// Cap returns the total buffer capacity.
func (q *AsyncIngestQueue) Cap() int {
	return cap(q.queue)
}
