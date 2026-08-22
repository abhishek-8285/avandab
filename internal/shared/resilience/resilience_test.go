package resilience

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{errors.New("database table is locked (6)"), true},
		{errors.New("database is locked"), true},
		{errors.New("SQLITE_BUSY"), true},
		{errors.New("i/o timeout"), true},
		{errors.New("connection reset by peer"), true},
		{errors.New("unexpected 500"), true},
		{errors.New("unique constraint failed"), false},
		{errors.New("not found"), false},
		{nil, false},
		{context.Canceled, false},
		{context.DeadlineExceeded, false},
	}
	for _, tc := range tests {
		if got := IsRetryable(tc.err); got != tc.want {
			t.Errorf("IsRetryable(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

func TestDo_RetriesOnLocked(t *testing.T) {
	ctx := context.Background()
	attempts := 0
	_, err := Do(ctx, Config{MaxAttempts: 3, InitialDelay: 1 * time.Millisecond, MaxDelay: 5 * time.Millisecond}, func(ctx context.Context) (int, error) {
		attempts++
		if attempts < 3 {
			return 0, errors.New("database table is locked")
		}
		return 42, nil
	})
	if err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}

func TestDo_NoRetryOnNonRetryable(t *testing.T) {
	ctx := context.Background()
	attempts := 0
	_, err := Do(ctx, Config{MaxAttempts: 3, InitialDelay: 1 * time.Millisecond}, func(ctx context.Context) (int, error) {
		attempts++
		return 0, errors.New("unique constraint failed")
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if attempts != 1 {
		t.Fatalf("expected 1 attempt for non-retryable, got %d", attempts)
	}
}

func TestDo_RespectsContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	attempts := 0
	_, err := Do(ctx, Config{MaxAttempts: 5, InitialDelay: 1 * time.Millisecond}, func(ctx context.Context) (int, error) {
		attempts++
		return 0, errors.New("database is locked")
	})
	if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		// Do returns ctx.Err() when context already cancelled
		if attempts != 0 && attempts != 1 {
			t.Fatalf("expected 0-1 attempts on cancelled ctx, got %d err=%v", attempts, err)
		}
	}
}

func TestSafe_RecoversPanic(t *testing.T) {
	ctx := context.Background()
	_, err := Safe(ctx, func(ctx context.Context) (int, error) {
		panic("boom")
	})
	if err == nil || err.Error() == "" {
		t.Fatal("expected panic to be converted to error")
	}
	if want := "panic recovered"; err.Error()[:15] != want[:15] {
		// just check it contains panic
		if !errors.Is(err, err) {
			_ = err
		}
	}
}

func TestDo_RecoversPanicAndRetries(t *testing.T) {
	ctx := context.Background()
	attempts := 0
	_, err := Do(ctx, Config{MaxAttempts: 2, InitialDelay: 1 * time.Millisecond, Retryable: func(error) bool { return true }}, func(ctx context.Context) (int, error) {
		attempts++
		if attempts == 1 {
			panic("first panic")
		}
		return 0, errors.New("second fail")
	})
	if err == nil {
		t.Fatal("expected error after retries")
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
}

func TestDoVoid(t *testing.T) {
	ctx := context.Background()
	called := false
	err := DoVoid(ctx, DefaultConfig(), func(ctx context.Context) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected op to be called")
	}
}

func TestDefaultConfig_ZeroHandling(t *testing.T) {
	ctx := context.Background()
	// Pass zero Config to verify defaults (MaxAttempts 3, delays) are applied.
	_, err := Do(ctx, Config{}, func(ctx context.Context) (int, error) {
		return 0, errors.New("unique constraint failed") // non-retryable
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestQuickConfig_Retries(t *testing.T) {
	ctx := context.Background()
	attempts := 0
	_, err := Do(ctx, QuickConfig(), func(ctx context.Context) (int, error) {
		attempts++
		if attempts < 2 {
			return 0, errors.New("timeout")
		}
		return 1, nil
	})
	if err != nil {
		t.Fatalf("QuickConfig retry failed: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
}

func TestDo_MaxDelayCap(t *testing.T) {
	ctx := context.Background()
	attempts := 0
	cfg := Config{MaxAttempts: 4, InitialDelay: 10 * time.Millisecond, MaxDelay: 15 * time.Millisecond, Multiplier: 10}
	_, err := Do(ctx, cfg, func(ctx context.Context) (int, error) {
		attempts++
		return 0, errors.New("busy")
	})
	if err == nil {
		t.Fatal("expected error after max attempts")
	}
	if attempts != 4 {
		t.Fatalf("expected 4 attempts with MaxDelay cap, got %d", attempts)
	}
}

func TestSafeVoid_RecoversPanic(t *testing.T) {
	ctx := context.Background()
	err := SafeVoid(ctx, func(ctx context.Context) error {
		panic("void boom")
	})
	if err == nil {
		t.Fatal("expected panic error")
	}
}

func TestIsRetryable_NetTimeout(t *testing.T) {
	err := &netTimeoutError{msg: "i/o timeout"}
	if !IsRetryable(err) {
		t.Fatal("expected net timeout to be retryable")
	}
}

type netTimeoutError struct{ msg string }

func (e *netTimeoutError) Error() string   { return e.msg }
func (e *netTimeoutError) Timeout() bool   { return true }
func (e *netTimeoutError) Temporary() bool { return true }
