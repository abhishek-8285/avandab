package telemetry

import (
	"context"
	"time"
)

// ShutdownTimeout bounds the full graceful shutdown: TCP stop + queue
// drain + HTTP shutdown share ONE deadline ctx created before the TCP
// stop (cmd/server/main.go). Single 30s budget — the old split (Drain 5s
// on its own Background timeout, then a fresh 30s ctx for srv.Shutdown)
// let the queue flush escape the HTTP deadline.
const ShutdownTimeout = 30 * time.Second

// ShutdownSequence stops ingest before HTTP, all inside one deadline:
// TCP listener first (prompt: Stop closes tracked conns), then queue
// drain, then HTTP shutdown. timeout <= 0 defaults to ShutdownTimeout.
// Nil steps are skipped. Returns the HTTP shutdown error, if any.
func ShutdownSequence(parent context.Context, timeout time.Duration, tcpStop func() error, queueDrain func(context.Context), httpShutdown func(context.Context) error) error {
	if timeout <= 0 {
		timeout = ShutdownTimeout
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	if tcpStop != nil {
		_ = tcpStop()
	}
	if queueDrain != nil {
		queueDrain(ctx)
	}
	if httpShutdown != nil {
		return httpShutdown(ctx)
	}
	return nil
}
