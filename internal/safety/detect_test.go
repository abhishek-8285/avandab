package safety

import (
	"testing"
	"time"
)

func testConfig() engineConfig {
	w := defaultWeights()
	return engineConfig{
		tickInterval:       DefaultTickInterval,
		speedLimitKmh:      80,
		brakeRateKmhS:      -8,
		accelRateKmhS:      8,
		accelMaxDTSecs:     10,
		minSpeedKmh:        20,
		idleMaxSpeedKmh:    2,
		idleMinutes:        5,
		nightStartHour:     23,
		nightEndHour:       5,
		nightMinSpeedKmh:   10,
		nightCooldownMin:   120,
		harshCooldownSec:   120,
		gapTolerance:       30 * time.Minute,
		weightSpeeding:     w[EventSpeeding],
		weightHarshBraking: w[EventHarshBraking],
		weightHarshAccel:   w[EventHarshAccel],
		weightIdling:       w[EventIdling],
		weightNightDriving: w[EventNightDriving],
	}
}

func snap(ts time.Time, speed float64, ignition *bool) snapshot {
	return snapshot{id: "s", vehicleID: "v1", driverID: "d1", ts: ts, speed: speed, ignition: ignition}
}

func boolPtr(b bool) *bool { return &b }

func TestDetectSpeedingRequiresConfirmation(t *testing.T) {
	cfg := testConfig()
	st := newState()
	t0 := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)

	detect(cfg, st, snap(t0, 60, nil), time.UTC)
	if evs := detect(cfg, st, snap(t0.Add(30*time.Second), 90, nil), time.UTC); len(evs) != 0 {
		t.Fatalf("first over-limit frame must not emit, got %v", evs)
	}
	evs := detect(cfg, st, snap(t0.Add(60*time.Second), 95, nil), time.UTC)
	if len(evs) != 1 || evs[0].eventType != EventSpeeding {
		t.Fatalf("second consecutive over-limit frame must emit one speeding event, got %v", evs)
	}
	if evs[0].severity != "medium" || evs[0].weight != 8 {
		t.Fatalf("unexpected severity/weight: %+v", evs[0])
	}
	if evs2 := detect(cfg, st, snap(t0.Add(90*time.Second), 100, nil), time.UTC); len(evs2) != 0 {
		t.Fatalf("same episode must not re-emit, got %v", evs2)
	}
	if evs3 := detect(cfg, st, snap(t0.Add(120*time.Second), 50, nil), time.UTC); len(evs3) != 0 {
		t.Fatalf("returning under limit must not emit, got %v", evs3)
	}
	if evs4 := detect(cfg, st, snap(t0.Add(150*time.Second), 85, nil), time.UTC); len(evs4) != 0 {
		t.Fatalf("confirmation frame of new episode must not emit yet, got %v", evs4)
	}
	if evs5 := detect(cfg, st, snap(t0.Add(180*time.Second), 82, nil), time.UTC); len(evs5) != 1 || evs5[0].severity != "low" {
		t.Fatalf("new episode after reset must emit low severity, got %v", evs5)
	}
}

func TestDetectHarshBraking(t *testing.T) {
	cfg := testConfig()
	st := newState()
	t0 := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)

	detect(cfg, st, snap(t0, 60, nil), time.UTC)
	if evs := detect(cfg, st, snap(t0.Add(3*time.Second), 45, nil), time.UTC); len(evs) != 0 {
		t.Fatalf("rate -5 kmh/s must stay under threshold, got %v", evs)
	}
	detect(cfg, st, snap(t0.Add(6*time.Second), 60, nil), time.UTC)
	evs := detect(cfg, st, snap(t0.Add(9*time.Second), 35, nil), time.UTC)
	if len(evs) != 1 || evs[0].eventType != EventHarshBraking {
		t.Fatalf("rate -8.33 kmh/s must emit harsh_braking, got %v", evs)
	}
	if evs[0].severity != "medium" {
		t.Fatalf("expected medium severity, got %s", evs[0].severity)
	}
}

func TestDetectHarshBrakingHighSeverityAndCooldown(t *testing.T) {
	cfg := testConfig()
	st := newState()
	t0 := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)

	detect(cfg, st, snap(t0, 70, nil), time.UTC)
	evs := detect(cfg, st, snap(t0.Add(3*time.Second), 32, nil), time.UTC)
	if len(evs) != 1 || evs[0].severity != "high" {
		t.Fatalf("rate ~-12.7 kmh/s must be high severity harsh_braking, got %v", evs)
	}
	detect(cfg, st, snap(t0.Add(6*time.Second), 70, nil), time.UTC)
	if evs := detect(cfg, st, snap(t0.Add(9*time.Second), 30, nil), time.UTC); len(evs) != 0 {
		t.Fatalf("cooldown must suppress repeat within %vs, got %v", cfg.harshCooldownSec, evs)
	}
}

func TestDetectHarshBrakingIgnoresSlowAndSparseFrames(t *testing.T) {
	cfg := testConfig()
	st := newState()
	t0 := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)

	detect(cfg, st, snap(t0, 15, nil), time.UTC)
	if evs := detect(cfg, st, snap(t0.Add(3*time.Second), 0, nil), time.UTC); len(evs) != 0 {
		t.Fatalf("braking below min_speed baseline must not emit, got %v", evs)
	}

	st2 := newState()
	detect(cfg, st2, snap(t0, 80, nil), time.UTC)
	if evs := detect(cfg, st2, snap(t0.Add(15*time.Second), 40, nil), time.UTC); len(evs) != 0 {
		t.Fatalf("frame gap beyond accel_max_dt must not compute rate, got %v", evs)
	}
}

func TestDetectHarshAccel(t *testing.T) {
	cfg := testConfig()
	st := newState()
	t0 := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)

	detect(cfg, st, snap(t0, 25, nil), time.UTC)
	evs := detect(cfg, st, snap(t0.Add(3*time.Second), 55, nil), time.UTC)
	if len(evs) != 1 || evs[0].eventType != EventHarshAccel || evs[0].severity != "medium" {
		t.Fatalf("rate +10 kmh/s must emit medium harsh_accel, got %v", evs)
	}
}

func TestDetectIdlingEmitsOncePerEpisode(t *testing.T) {
	cfg := testConfig()
	st := newState()
	t0 := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	ts := t0
	var got []detectedEvent
	for i := 0; i < 12; i++ {
		got = append(got, detect(cfg, st, snap(ts, 0, boolPtr(true)), time.UTC)...)
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
		detect(cfg, st2, snap(ts, 0, boolPtr(false)), time.UTC)
		ts = ts.Add(30 * time.Second)
	}
	if evs := st2.lastEventAt; len(evs) != 0 {
		t.Fatalf("ignition off must never idle-emit, got %v", evs)
	}
}

func TestDetectNightDrivingCooldownAndDaylight(t *testing.T) {
	cfg := testConfig()
	st := newState()
	night := time.Date(2026, 8, 20, 23, 30, 0, 0, time.UTC)

	if evs := detect(cfg, st, snap(night, 40, nil), time.UTC); len(evs) != 1 || evs[0].eventType != EventNightDriving {
		t.Fatalf("moving at 23:30 must emit night_driving, got %v", evs)
	}
	if evs := detect(cfg, st, snap(night.Add(10*time.Minute), 42, nil), time.UTC); len(evs) != 0 {
		t.Fatalf("night cooldown must suppress repeats, got %v", evs)
	}
	day := time.Date(2026, 8, 21, 14, 0, 0, 0, time.UTC)
	if evs := detect(cfg, st, snap(day, 60, nil), time.UTC); len(evs) != 0 {
		t.Fatalf("daylight driving must not emit, got %v", evs)
	}
}

func TestDetectGapResetsEpisodes(t *testing.T) {
	cfg := testConfig()
	st := newState()
	t0 := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)

	detect(cfg, st, snap(t0, 60, nil), time.UTC)
	detect(cfg, st, snap(t0.Add(30*time.Second), 0, boolPtr(true)), time.UTC)
	if evs := detect(cfg, st, snap(t0.Add(2*time.Hour), 0, boolPtr(true)), time.UTC); len(evs) != 0 {
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
