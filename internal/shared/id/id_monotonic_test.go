package id_test

import (
	"testing"

	"transport-app/internal/shared/id"
)

// #16: GenerateDisplayID used uuid[:8] (32 bits): random, non-monotonic and
// collision-prone at ~77k rows. Sequential IDs must be distinct and ordered.
func TestGenerateDisplayID_MonotonicNoCollision(t *testing.T) {
	g := id.NewUUIDGenerator()
	const n = 50
	seen := map[string]bool{}
	prev := ""
	for i := 0; i < n; i++ {
		cur := g.GenerateDisplayID("INV")
		if seen[cur] {
			t.Fatalf("duplicate display ID %q on call %d", cur, i)
		}
		seen[cur] = true
		if i > 0 && cur <= prev {
			t.Fatalf("not monotonically increasing: %q then %q", prev, cur)
		}
		prev = cur
	}
}
