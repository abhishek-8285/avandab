package errors

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Regression: ids used to be `err_<time.Now().UnixNano()>` with nothing else.
// `time.Now()` does NOT advance every nanosecond — on Windows it advances in
// ~0.5ms steps (measured: 20/20 back-to-back calls returned the identical
// value), so two error reports raised inside one tick produced the same id and
// the second INSERT died on `UNIQUE constraint failed: error_reports.id`.
//
// That made TestOpsErrorsAPIGetError fail ~25% of the time on a Windows dev
// box (10 failures in 40 runs at HEAD) while staying green on Linux CI, which
// has ns-resolution clocks. The fix must not depend on clock resolution at all,
// so this asserts the property directly instead of racing the clock.
func TestNewID_UniqueWithinASingleClockTick(t *testing.T) {
	const n = 100
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		id := newID("err")
		require.NotEmpty(t, id)
		require.True(t, strings.HasPrefix(id, "err_"), "id %q must keep the err_ prefix", id)
		_, dup := seen[id]
		require.False(t, dup, "duplicate id %q on call %d — id depends on clock resolution", id, i)
		seen[id] = struct{}{}
	}
}

func TestNewID_DistinctPrefixesAndMonotonicCounter(t *testing.T) {
	a := newID("err")
	b := newID("inc")
	require.NotEqual(t, a, b)
	require.True(t, strings.HasPrefix(a, "err_"))
	require.True(t, strings.HasPrefix(b, "inc_"))
}
