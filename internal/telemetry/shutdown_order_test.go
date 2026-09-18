package telemetry

import (
	"context"
	"testing"
	"time"
)

// Red-green ownership for server shutdown ordering (cmd/server/main.go
// ~:2051-2055): TCP stop -> queue drain -> HTTP shutdown must share ONE
// deadline ctx created BEFORE the TCP stop. Pre-fix Drain(5s) owned a
// separate Background timeout and srv.Shutdown owned a 30s ctx created
// after — no shared deadline, no testable order.
func TestShutdownSequence_OrderAndSharedDeadline(t *testing.T) {
	var order []string
	var drainDeadline, httpDeadline time.Time
	var drainOK, httpOK bool

	tcpStop := func() error {
		order = append(order, "tcp-stop")
		return nil
	}
	queueDrain := func(ctx context.Context) {
		order = append(order, "queue-drain")
		drainDeadline, drainOK = ctx.Deadline()
	}
	httpShutdown := func(ctx context.Context) error {
		order = append(order, "http-shutdown")
		httpDeadline, httpOK = ctx.Deadline()
		return nil
	}

	timeout := 30 * time.Second
	if err := ShutdownSequence(context.Background(), timeout, tcpStop, queueDrain, httpShutdown); err != nil {
		t.Fatalf("ShutdownSequence: %v", err)
	}

	want := []string{"tcp-stop", "queue-drain", "http-shutdown"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v (drain must precede HTTP shutdown)", order, want)
		}
	}

	if !drainOK || !httpOK {
		t.Fatalf("shared deadline missing: drainOK=%v httpOK=%v (both must inherit one timeout ctx)", drainOK, httpOK)
	}
	if !drainDeadline.Equal(httpDeadline) {
		t.Fatalf("drain deadline %v != http deadline %v (must share one ctx)", drainDeadline, httpDeadline)
	}
	if until := time.Until(drainDeadline); until <= 0 || until > timeout {
		t.Fatalf("shared deadline in %v, want (0, %v]", until, timeout)
	}
}

// Start must honor a signal-scoped parent ctx: cancelling the parent
// (SIGINT/SIGTERM) has to stop workers so a later drain never hangs.
// Pre-fix main.go passed context.Background() to Start, so this
// cancellation path was dead in prod.
func TestAsyncIngestQueue_StartRespectsSignalCancel(t *testing.T) {
	sigCtx, stop := context.WithCancel(context.Background())

	q := NewAsyncIngestQueue(10, 1, nil, nil)
	q.Start(sigCtx)
	stop() // simulate SIGINT/SIGTERM

	done := make(chan struct{})
	go func() { q.Drain(2 * time.Second); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Drain blocked >3s after signal-cancel — workers ignore parent ctx")
	}
}

// DrainContext must honor the shared shutdown ctx: a cancelled parent
// aborts the drain instead of flushing on its own Background timeout.
func TestAsyncIngestQueue_DrainContextHonorsSharedDeadline(t *testing.T) {
	q := NewAsyncIngestQueue(10, 0, nil, nil) // unstarted: nobody consumes
	for i := 0; i < 5; i++ {
		if !q.Push(RawFrame{IMEI: "X"}) {
			t.Fatal("push must succeed")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // shutdown deadline already fired

	start := time.Now()
	q.DrainContext(ctx)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("DrainContext took %v after cancelled ctx, want immediate return", elapsed)
	}
}
