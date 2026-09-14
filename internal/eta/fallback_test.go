package eta

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFreshnessFallbackReason(t *testing.T) {
	const staleMin = 15
	tests := []struct {
		name    string
		present bool
		age     time.Duration
		reason  string
		fb      bool
	}{
		{"no snapshot", false, 0, "no_telemetry", true},
		{"no snapshot ignores age", false, 60 * time.Minute, "no_telemetry", true},
		{"stale snapshot", true, 20 * time.Minute, "stale_telemetry", true},
		{"exactly staleMin is fresh", true, 15 * time.Minute, "", false},
		{"just under staleMin is fresh", true, 14*time.Minute + 59*time.Second, "", false},
		{"fresh snapshot", true, 2 * time.Minute, "", false},
		{"future snapshot clock skew is fresh", true, -time.Minute, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason, fb := freshnessFallbackReason(tt.present, tt.age, staleMin)
			require.Equal(t, tt.reason, reason)
			require.Equal(t, tt.fb, fb)
		})
	}
}

func TestSamplesFallbackReason(t *testing.T) {
	tests := []struct {
		name   string
		smpErr bool
		count  int
		speed  float64
		reason string
		fb     bool
	}{
		{"query error", true, 10, 50.0, "insufficient_samples", true},
		{"zero samples", false, 0, 50.0, "insufficient_samples", true},
		{"two samples", false, 2, 50.0, "insufficient_samples", true},
		{"three samples", false, 3, 50.0, "", false},
		{"zero speed", false, 5, 0.0, "insufficient_samples", true},
		{"negative speed", false, 5, -1.0, "insufficient_samples", true},
		{"healthy", false, 5, 50.0, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason, fb := samplesFallbackReason(tt.smpErr, tt.count, tt.speed)
			require.Equal(t, tt.reason, reason)
			require.Equal(t, tt.fb, fb)
		})
	}
}

func TestOdometerRemaining(t *testing.T) {
	tests := []struct {
		name                 string
		route, latest, start float64
		want                 float64
		wantOK               bool
	}{
		{"normal delta", 100, 1040, 1000, 60, true},
		{"negative delta (reset/swap)", 100, 990, 1000, 0, false},
		{"unknown route", 0, 1040, 1000, 0, false},
		{"fully travelled", 100, 1100, 1000, 0, true},
		{"over-travelled clamps to zero", 100, 1200, 1000, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := odometerRemaining(tt.route, tt.latest, tt.start)
			require.Equal(t, tt.wantOK, ok)
			require.InDelta(t, tt.want, got, 1e-9)
		})
	}
}

func TestTimePropRemaining(t *testing.T) {
	tests := []struct {
		name                string
		route, elapsed, est float64
		want                float64
		wantOK              bool
	}{
		{"halfway", 100, 1, 2, 50, true},
		{"zero elapsed", 100, 0, 2, 0, false},
		{"negative elapsed (clock skew)", 100, -1, 2, 0, false},
		{"zero estimate", 100, 1, 0, 0, false},
		{"zero route", 0, 1, 2, 0, false},
		{"past estimate clamps to zero", 100, 3, 2, 0, true},
		{"exactly at estimate", 100, 2, 2, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := timePropRemaining(tt.route, tt.elapsed, tt.est)
			require.Equal(t, tt.wantOK, ok)
			require.InDelta(t, tt.want, got, 1e-9)
		})
	}
}
