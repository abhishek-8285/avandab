package safety

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func testPolicy() SafetyPolicy {
	return DefaultSafetyPolicy("tenant-1")
}

func snap(ts time.Time, speed float64, ignition *bool) snapshot {
	return snapshot{id: "s", vehicleID: "v1", driverID: "d1", ts: ts, speed: speed, ignition: ignition}
}

func snapWithGeo(ts time.Time, speed, lat, lng, acc float64, ignition *bool) snapshot {
	return snapshot{
		id:        "s",
		vehicleID: "v1",
		driverID:  "d1",
		ts:        ts,
		speed:     speed,
		lat:       lat,
		lng:       lng,
		accuracy:  acc,
		ignition:  ignition,
	}
}

func boolPtr(b bool) *bool { return &b }

func TestDetectSpeedingRequiresConfirmation(t *testing.T) {
	p := testPolicy()
	st := newState()
	t0 := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)

	detect(p, st, snap(t0, 60, nil), time.UTC)
	if evs := detect(p, st, snap(t0.Add(30*time.Second), 90, nil), time.UTC); len(evs) != 0 {
		t.Fatalf("first over-limit frame must not emit, got %v", evs)
	}
	evs := detect(p, st, snap(t0.Add(60*time.Second), 95, nil), time.UTC)
	if len(evs) != 1 || evs[0].eventType != EventSpeeding {
		t.Fatalf("second consecutive over-limit frame must emit one speeding event, got %v", evs)
	}
	if evs[0].severity != "medium" || evs[0].weight != 8 {
		t.Fatalf("unexpected severity/weight: %+v", evs[0])
	}
	if evs2 := detect(p, st, snap(t0.Add(90*time.Second), 100, nil), time.UTC); len(evs2) != 0 {
		t.Fatalf("same episode must not re-emit, got %v", evs2)
	}
	if evs3 := detect(p, st, snap(t0.Add(120*time.Second), 50, nil), time.UTC); len(evs3) != 0 {
		t.Fatalf("returning under limit must not emit, got %v", evs3)
	}
	if evs4 := detect(p, st, snap(t0.Add(150*time.Second), 85, nil), time.UTC); len(evs4) != 0 {
		t.Fatalf("confirmation frame of new episode must not emit yet, got %v", evs4)
	}
	if evs5 := detect(p, st, snap(t0.Add(180*time.Second), 82, nil), time.UTC); len(evs5) != 1 || evs5[0].severity != "low" {
		t.Fatalf("new episode after reset must emit low severity, got %v", evs5)
	}
}

func TestDetectSpeedingBelowThresholdIgnored(t *testing.T) {
	p := testPolicy()
	p.SpeedLimitKmh = 80.0
	st := newState()
	t0 := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)

	detect(p, st, snap(t0, 60, nil), time.UTC)
	evs := detect(p, st, snap(t0.Add(30*time.Second), 79.5, nil), time.UTC)
	assert.Empty(t, evs, "speed below limit must be ignored")
	evs = detect(p, st, snap(t0.Add(60*time.Second), 80.0, nil), time.UTC)
	assert.Empty(t, evs, "speed exactly at limit must be ignored")
}

func TestDetectHarshBraking(t *testing.T) {
	p := testPolicy()
	st := newState()
	t0 := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)

	detect(p, st, snap(t0, 60, nil), time.UTC)
	if evs := detect(p, st, snap(t0.Add(3*time.Second), 45, nil), time.UTC); len(evs) != 0 {
		t.Fatalf("rate -5 kmh/s must stay under threshold, got %v", evs)
	}
	detect(p, st, snap(t0.Add(6*time.Second), 60, nil), time.UTC)
	evs := detect(p, st, snap(t0.Add(9*time.Second), 35, nil), time.UTC)
	if len(evs) != 1 || evs[0].eventType != EventHarshBraking {
		t.Fatalf("rate -8.33 kmh/s must emit harsh_braking, got %v", evs)
	}
	if evs[0].severity != "medium" {
		t.Fatalf("expected medium severity, got %s", evs[0].severity)
	}
}

func TestDetectHarshBrakingHighSeverityAndCooldown(t *testing.T) {
	p := testPolicy()
	st := newState()
	t0 := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)

	detect(p, st, snap(t0, 70, nil), time.UTC)
	evs := detect(p, st, snap(t0.Add(3*time.Second), 32, nil), time.UTC)
	if len(evs) != 1 || evs[0].severity != "high" {
		t.Fatalf("rate ~-12.7 kmh/s must be high severity harsh_braking, got %v", evs)
	}
	detect(p, st, snap(t0.Add(6*time.Second), 70, nil), time.UTC)
	if evs := detect(p, st, snap(t0.Add(9*time.Second), 30, nil), time.UTC); len(evs) != 0 {
		t.Fatalf("cooldown must suppress repeat within %vs, got %v", p.HarshCooldownSec, evs)
	}
}

func TestDetectHarshBrakingIgnoresSlowAndSparseFrames(t *testing.T) {
	p := testPolicy()
	st := newState()
	t0 := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)

	detect(p, st, snap(t0, 15, nil), time.UTC)
	if evs := detect(p, st, snap(t0.Add(3*time.Second), 0, nil), time.UTC); len(evs) != 0 {
		t.Fatalf("braking below min_speed baseline must not emit, got %v", evs)
	}

	st2 := newState()
	detect(p, st2, snap(t0, 80, nil), time.UTC)
	if evs := detect(p, st2, snap(t0.Add(15*time.Second), 40, nil), time.UTC); len(evs) != 0 {
		t.Fatalf("frame gap beyond accel_max_dt must not compute rate, got %v", evs)
	}
}

func TestDetectHarshAccel(t *testing.T) {
	p := testPolicy()
	st := newState()
	t0 := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)

	detect(p, st, snap(t0, 25, nil), time.UTC)
	evs := detect(p, st, snap(t0.Add(3*time.Second), 55, nil), time.UTC)
	if len(evs) != 1 || evs[0].eventType != EventHarshAccel || evs[0].severity != "medium" {
		t.Fatalf("rate +10 kmh/s must emit medium harsh_accel, got %v", evs)
	}
}

func TestDetectIdlingEmitsOncePerEpisode(t *testing.T) {
	p := testPolicy()
	st := newState()
	t0 := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	ts := t0
	var got []detectedEvent
	for i := 0; i < 12; i++ {
		got = append(got, detect(p, st, snap(ts, 0, boolPtr(true)), time.UTC)...)
		ts = ts.Add(30 * time.Second)
	}
	if len(got) != 1 || got[0].eventType != EventIdling {
		t.Fatalf("6-minute idle with ignition on must emit exactly one idling event, got %v", got)
	}
	if got[0].severity != "low" || got[0].weight != 3 {
		t.Fatalf("unexpected idling severity/weight: %+v", got[0])
	}

	st2 := newState()
	ts = t0
	for i := 0; i < 12; i++ {
		detect(p, st2, snap(ts, 0, boolPtr(false)), time.UTC)
		ts = ts.Add(30 * time.Second)
	}
	if evs := st2.lastEventAt; len(evs) != 0 {
		t.Fatalf("ignition off must never idle-emit, got %v", evs)
	}
}

func TestDetectNightDrivingCooldownAndDaylight(t *testing.T) {
	p := testPolicy()
	st := newState()
	night := time.Date(2026, 8, 20, 23, 30, 0, 0, time.UTC)

	if evs := detect(p, st, snap(night, 40, nil), time.UTC); len(evs) != 1 || evs[0].eventType != EventNightDriving {
		t.Fatalf("moving at 23:30 must emit night_driving, got %v", evs)
	}
	if evs := detect(p, st, snap(night.Add(10*time.Minute), 42, nil), time.UTC); len(evs) != 0 {
		t.Fatalf("night cooldown must suppress repeats, got %v", evs)
	}
	day := time.Date(2026, 8, 21, 14, 0, 0, 0, time.UTC)
	if evs := detect(p, st, snap(day, 60, nil), time.UTC); len(evs) != 0 {
		t.Fatalf("daylight driving must not emit, got %v", evs)
	}
}

func TestDetectPoorGPSAccuracyIgnored(t *testing.T) {
	p := testPolicy()
	p.MaxAccuracyMeters = 50.0
	st := newState()
	t0 := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)

	detect(p, st, snapWithGeo(t0, 30, 28.6139, 77.2090, 10.0, nil), time.UTC)

	// Low accuracy reading (accuracy = 120m) with high delta speed must NOT trigger false harsh accel/speeding
	evs := detect(p, st, snapWithGeo(t0.Add(3*time.Second), 90, 28.6140, 77.2091, 120.0, nil), time.UTC)
	assert.Empty(t, evs, "poor GPS accuracy frame (>50m) must be ignored to prevent false positives")
}

func TestDetectImpossibleGPSJumpIgnored(t *testing.T) {
	p := testPolicy()
	p.MaxFeasibleSpeedKmh = 180.0
	st := newState()
	t0 := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)

	// Delhi point
	detect(p, st, snapWithGeo(t0, 40, 28.6139, 77.2090, 10.0, nil), time.UTC)

	// Mumbai point (1100 km away) in 3 seconds (teleportation / cell tower hop)
	evs := detect(p, st, snapWithGeo(t0.Add(3*time.Second), 100, 19.0760, 72.8777, 10.0, nil), time.UTC)
	assert.Empty(t, evs, "impossible GPS jump (>180 km/h implied speed) must be ignored")
}

func TestDetectGapResetsEpisodes(t *testing.T) {
	p := testPolicy()
	st := newState()
	t0 := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)

	detect(p, st, snap(t0, 60, nil), time.UTC)
	detect(p, st, snap(t0.Add(30*time.Second), 0, boolPtr(true)), time.UTC)
	if evs := detect(p, st, snap(t0.Add(2*time.Hour), 0, boolPtr(true)), time.UTC); len(evs) != 0 {
		t.Fatalf("reconnect frame across a gap must not emit, got %v", evs)
	}
	if !st.idleStart.IsZero() {
		t.Fatal("gap must reset idle tracking")
	}
}

func TestIsNightWrapsMidnight(t *testing.T) {
	cases := []struct {
		hour  int
		start float64
		end   float64
		want  bool
	}{
		{23, 23, 5, true},
		{2, 23, 5, true},
		{4, 23, 5, true},
		{5, 23, 5, false},
		{12, 23, 5, false},
		{12, 2, 4, false},
		{3, 2, 4, true},
	}
	for _, c := range cases {
		ts := time.Date(2026, 8, 20, c.hour, 0, 0, 0, time.UTC)
		if got := isNight(ts, c.start, c.end, time.UTC); got != c.want {
			t.Errorf("isNight(hour=%d, %v..%v) = %v, want %v", c.hour, c.start, c.end, got, c.want)
		}
	}
}
