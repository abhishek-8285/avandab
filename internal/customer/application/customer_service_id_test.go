package application

import (
	"testing"

	"transport-app/internal/shared/id"
)

// Regression: booking numbers used to be
// `fmt.Sprintf("BKG-%d", time.Now().UnixNano()%1000000)`.
//
// bookings.booking_number is `TEXT NOT NULL UNIQUE`, and time.Now() does NOT
// advance every nanosecond -- on Windows it steps in ~0.5ms chunks (measured:
// 20/20 back-to-back UnixNano() calls returned an identical value). Two
// bookings minted inside one tick therefore produced the SAME number and the
// second INSERT failed, taking the whole request with it. The `%1000000`
// truncation made the space smaller still.
//
// The failure is clock-dependent, so it hides on Linux CI (ns-resolution
// clocks) and only shows up intermittently on Windows -- which is what makes
// it a long-lived flaky-test generator rather than an obvious bug. Assert the
// property directly instead of racing the clock.
func TestNextBookingNumber_UniqueWithinOneClockTick(t *testing.T) {
	svc := NewCustomerAppService(nil, id.NewUUIDGenerator())

	const n = 100
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		got := svc.nextBookingNumber()
		if _, dup := seen[got]; dup {
			t.Fatalf("duplicate booking number %q on call %d -- id depends on clock resolution", got, i)
		}
		seen[got] = struct{}{}
	}
}
