package telemetry

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"transport-app/internal/events"
	"transport-app/internal/shared/id"
	"transport-app/internal/shared/ports"
)

// blockingUoW fakes a stuck DB write: Execute blocks until ctx is cancelled.
// Pre-fix workers use the Start ctx (Background in prod, never cancelled),
// so Drain hangs in unbounded wg.Wait. Post-fix Drain deadline-cancels the
// worker ctx, unblocking Execute.
type blockingUoW struct {
	entered chan struct{}
	once    sync.Once
}

func (b *blockingUoW) Execute(ctx context.Context, fn func(txCtx ports.TxContext) error) error {
	b.once.Do(func() { close(b.entered) })
	<-ctx.Done()
	return ctx.Err()
}

// Stop must close idle accepted conns promptly, not wait out the 5-min read deadline.
func TestTCPIngestServer_StopClosesActiveConnsPromptly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := events.NewInMemoryBus()
	ing := &Ingestor{bus: bus, cfg: IngestConfig{}}
	ing.SetQueue(NewAsyncIngestQueue(100, 1, nil, nil))

	srv := NewTCPIngestServer("127.0.0.1:0", ing, nil, nil)
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	conn, err := net.DialTimeout("tcp", srv.listener.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	time.Sleep(300 * time.Millisecond) // let server Accept block in Read

	done := make(chan struct{})
	go func() { _ = srv.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("Stop blocked >3s with idle conn — active conns not closed (5-min read deadline)")
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Fatalf("client read got data after Stop, want conn closed")
	}
}

// Drain must return within its timeout even when a worker is stuck in IngestRawFrame.
func TestAsyncIngestQueue_DrainDeadlineBounded(t *testing.T) {
	db := newTestIngestorDB(t)
	bu := &blockingUoW{entered: make(chan struct{})}
	ing := NewIngestor(db, bu, events.NewInMemoryBus(), id.NewUUIDGenerator(), &testAudit{}, IngestConfig{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q := NewAsyncIngestQueue(10, 1, ing, nil)
	q.Start(ctx)
	if !q.Push(RawFrame{IMEI: "BLOCK-1", Latitude: 1, Longitude: 1, DeviceTime: time.Now().UTC()}) {
		t.Fatal("push must succeed")
	}
	select {
	case <-bu.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not pick up frame")
	}

	done := make(chan struct{})
	start := time.Now()
	go func() { q.Drain(300 * time.Millisecond); close(done) }()
	select {
	case <-done:
		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Fatalf("Drain took %v, want <2s (deadline-bounded)", elapsed)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("Drain blocked >3s with stuck worker — wg.Wait unbounded, worker ctx not deadline-cancelled")
	}
}

// Drain must surface ingest errors instead of discarding them.
func TestAsyncIngestQueue_DrainSurfacesIngestErrors(t *testing.T) {
	db := newTestIngestorDB(t)
	ing := newTestIngestor(t, db, nil)
	_ = db.Close() // every pipeline query fails

	q := NewAsyncIngestQueue(10, 1, ing, nil) // unstarted: Drain owns the queue
	for i := 0; i < 3; i++ {
		if !q.Push(RawFrame{IMEI: "FAIL-DRAIN", Latitude: 1, Longitude: 1, DeviceTime: time.Now().UTC()}) {
			t.Fatal("push must succeed")
		}
	}
	q.Drain(2 * time.Second)
	if got := q.FailedCount(); got != 3 {
		t.Fatalf("failedCount = %d, want 3 — drain discarded ingest errors", got)
	}
}
