// Package resilience provides a single, reusable wrapper for all service calls.
// Every new service MUST use it — it guarantees: no panic crashes the server,
// transient errors are retried with backoff, and timeouts/context cancellation
// are respected. Pattern: shared.TenantIDFromContext(ctx) already exists for
// tenancy; this package adds the operational safety layer on top.
//
// Usage for any future service:
//
//	import "transport-app/internal/shared/resilience"
//
//	// Simple retry (DB busy/locked, timeout, 5xx):
//	result, err := resilience.Do(ctx, resilience.DefaultConfig(), func(ctx context.Context) (MyResult, error) {
//	    return repo.Query(ctx, arg)
//	})
//
//	// Panic-safe (recovers, logs, returns error instead of crashing):
//	result, err := resilience.Safe(ctx, func(ctx context.Context) (MyResult, error) {
//	    return riskyService.Call(ctx)
//	})
//
//	// HTTP integration with retry + timeout:
//	result, err := resilience.Do(ctx, resilience.QuickConfig(), func(ctx context.Context) (MyResult, error) {
//	    return httpClient.DoRequest(ctx)
//	})
package resilience

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"runtime/debug"
	"strings"
	"time"
)

// Config controls retry behaviour. Zero values are replaced by defaults in Do.
type Config struct {
	MaxAttempts  int
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Multiplier   float64
	Retryable    func(error) bool // nil => IsRetryable
	Logger       *slog.Logger
}

// DefaultConfig is tuned for DB and internal service calls.
// 3 attempts, 100ms→400ms exponential, retries on busy/locked/timeout.
func DefaultConfig() Config {
	return Config{
		MaxAttempts:  3,
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     1 * time.Second,
		Multiplier:   2.0,
	}
}

// QuickConfig is for fast external HTTP calls (Fastag/Ewaybill/Razorpay).
// Shorter delays, same retry predicate (5xx/timeout).
func QuickConfig() Config {
	return Config{
		MaxAttempts:  3,
		InitialDelay: 200 * time.Millisecond,
		MaxDelay:     800 * time.Millisecond,
		Multiplier:   2.0,
	}
}

// IsRetryable reports whether err is transient and worth retrying.
// Covers: SQLITE_BUSY/locked, deadline/timeout, temporary net errors, 5xx-like strings.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return false // caller cancelled — never retry
	}
	var nerr net.Error
	if errors.As(err, &nerr) && nerr.Timeout() {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, s := range []string{
		"locked",   // SQLITE_LOCKED (6), database table is locked
		"busy",     // SQLITE_BUSY (5), database is locked
		"deadline", // deadline exceeded
		"timeout",  // i/o timeout
		"temporar", // temporary
		"connection refused",
		"connection reset",
		"too many requests",            // 429
		" 500", " 502", " 503", " 504", // HTTP 5xx in error strings
	} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// Do executes op with retry + panic recovery. On each failure it checks
// Retryable (or IsRetryable) and sleeps with exponential backoff until
// MaxAttempts or context cancellation. Panics are recovered and returned as errors.
func Do[T any](ctx context.Context, cfg Config, op func(context.Context) (T, error)) (T, error) {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 3
	}
	if cfg.InitialDelay <= 0 {
		cfg.InitialDelay = 100 * time.Millisecond
	}
	if cfg.MaxDelay <= 0 {
		cfg.MaxDelay = 1 * time.Second
	}
	if cfg.Multiplier <= 0 {
		cfg.Multiplier = 2.0
	}
	retryable := cfg.Retryable
	if retryable == nil {
		retryable = IsRetryable
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	var zero T
	var lastErr error
	delay := cfg.InitialDelay

	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		result, err := safeCall(ctx, op)
		if err == nil {
			return result, nil
		}
		lastErr = err

		// Never retry on context cancellation.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return zero, err
		}
		if ctx.Err() != nil {
			return zero, ctx.Err()
		}
		if !retryable(err) {
			return zero, err
		}
		if attempt == cfg.MaxAttempts {
			break
		}

		logger.Debug("resilience: retrying transient failure",
			"attempt", attempt,
			"max_attempts", cfg.MaxAttempts,
			"delay", delay,
			"error", err,
		)

		select {
		case <-ctx.Done():
			return zero, ctx.Err()
		case <-time.After(delay):
		}

		delay = time.Duration(float64(delay) * cfg.Multiplier)
		if delay > cfg.MaxDelay {
			delay = cfg.MaxDelay
		}
	}
	return zero, fmt.Errorf("resilience: failed after %d attempts: %w", cfg.MaxAttempts, lastErr)
}

// DoVoid is Do for operations with no return value.
func DoVoid(ctx context.Context, cfg Config, op func(context.Context) error) error {
	_, err := Do(ctx, cfg, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, op(ctx)
	})
	return err
}

// Safe executes op with panic recovery but no retry. Use when the operation
// is not retryable or you want only crash protection.
func Safe[T any](ctx context.Context, op func(context.Context) (T, error)) (T, error) {
	return safeCall(ctx, op)
}

// SafeVoid is Safe for void operations.
func SafeVoid(ctx context.Context, op func(context.Context) error) error {
	_, err := safeCall(ctx, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, op(ctx)
	})
	return err
}

func safeCall[T any](ctx context.Context, op func(context.Context) (T, error)) (res T, err error) {
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			slog.Error("resilience: panic recovered",
				"panic", fmt.Sprintf("%v", r),
				"stack", string(stack),
			)
			var zero T
			res = zero
			err = fmt.Errorf("panic recovered: %v", r)
		}
	}()
	return op(ctx)
}
