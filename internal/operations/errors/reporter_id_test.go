package errors

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"transport-app/internal/shared/ports"
)

// seqID is a deterministic ports.IDGenerator: every call returns the next
// sequence value. If ids stay unique under a constant generator output shape,
// uniqueness cannot depend on clock resolution.
type seqID struct {
	n int
}

func (s *seqID) GenerateUUID() string {
	s.n++
	return fmt.Sprintf("uuid-%d", s.n)
}

func (s *seqID) GenerateDisplayID(prefix string) string {
	return prefix + "-" + s.GenerateUUID()[:8]
}

// fixedClock is a deterministic ports.Clock stuck on one instant.
type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

// Compile-time seam check: the fakes above must satisfy the ports.
func TestSeamFakesSatisfyPorts(t *testing.T) {
	var (
		_ ports.IDGenerator = (*seqID)(nil)
		_ ports.Clock       = fixedClock{}
	)
}

// Regression: ids used to be `err_<time.Now().UnixNano()>` with nothing else.
// `time.Now()` does NOT advance every nanosecond — on Windows it advances in
// ~0.5ms steps (measured: 20/20 back-to-back calls returned the identical
// value), so two error reports raised inside one tick produced the same id and
// the second INSERT died on `UNIQUE constraint failed: error_reports.id`.
//
// That made TestOpsErrorsAPIGetError fail ~25% of the time on a Windows dev
// box (10 failures in 40 runs at HEAD) while staying green on Linux CI, which
// has ns-resolution clocks. Ids now come from the injected IDGenerator, so
// this pins the seam instead of racing the clock.
func TestNewID_UniqueWithinASingleClockTick(t *testing.T) {
	rep := NewReporter(nil, nil, "test", "v1", &seqID{}, fixedClock{t: time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)})

	const n = 100
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		id := rep.newID("err")
		require.NotEmpty(t, id)
		require.True(t, strings.HasPrefix(id, "err_"), "id %q must keep the err_ prefix", id)
		_, dup := seen[id]
		require.False(t, dup, "duplicate id %q on call %d — id depends on clock resolution", id, i)
		seen[id] = struct{}{}
	}
}

func TestNewID_DistinctPrefixes(t *testing.T) {
	rep := NewReporter(nil, nil, "test", "v1", &seqID{}, fixedClock{})
	a := rep.newID("err")
	b := rep.newID("inc")
	require.NotEqual(t, a, b)
	require.True(t, strings.HasPrefix(a, "err_"))
	require.True(t, strings.HasPrefix(b, "inc_"))
}

// The injected clock stamps reports and incidents: frozen clock in, frozen
// timestamps out. Pre-seam this was untestable (time.Now() inline).
func TestReporter_UsesInjectedClock(t *testing.T) {
	frozen := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	rep := NewReporter(nil, nil, "test", "v1", &seqID{}, fixedClock{t: frozen})

	got, err := rep.Report(context.Background(), ErrorReport{Method: "POST", URL: "/api/v1/bookings", Message: "boom"})
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(got.ID, "err_"))
	require.True(t, got.Timestamp.Equal(frozen), "timestamp %v must come from injected clock", got.Timestamp)
}
