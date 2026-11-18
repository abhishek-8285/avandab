package telemetry

import "testing"

// Unknown provider names must still quarantine (source CHECK allows only three
// values); the batch must not abort.
func TestQuarantineSource_MapsUnknownProvider(t *testing.T) {
	for _, in := range []string{"gt06", "ais140", "teltonika", "mobile", ""} {
		if got := quarantineSource(in); got != "own" {
			t.Fatalf("quarantineSource(%q) = %q, want own", in, got)
		}
	}
	if got := quarantineSource("loconav"); got != "loconav" {
		t.Fatalf("known provider changed: %q", got)
	}
}
