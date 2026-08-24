package safety

import (
	"fmt"
	"time"
)

const (
	EventSpeeding     = "speeding"
	EventHarshBraking = "harsh_braking"
	EventHarshAccel   = "harsh_accel"
	EventIdling       = "idling"
	EventNightDriving = "night_driving"
)

type snapshot struct {
	id        string
	tripID    string
	vehicleID string
	driverID  string
	ts        time.Time
	speed     float64
	ignition  *bool
}

type detectedEvent struct {
	eventType  string
	severity   string
	weight     float64
	metadata   string
	occurredAt time.Time
}

type vehicleState struct {
	watermark   time.Time
	lastSpeed   float64
	hasLast     bool
	speedStreak int
	speeding    bool
	idleStart   time.Time
	lastEventAt map[string]time.Time
	driverID    string
}

func newState() *vehicleState {
	return &vehicleState{lastEventAt: make(map[string]time.Time)}
}

func (st *vehicleState) onCooldown(eventType string, ts time.Time, cooldown time.Duration) bool {
	last, ok := st.lastEventAt[eventType]
	return ok && ts.Sub(last) < cooldown
}

func (st *vehicleState) markEvent(eventType string, ts time.Time) {
	st.lastEventAt[eventType] = ts
}

func isNight(ts time.Time, startHour, endHour float64, loc *time.Location) bool {
	h := float64(ts.In(loc).Hour()) + float64(ts.In(loc).Minute())/60.0
	if startHour <= endHour {
		return h >= startHour && h < endHour
	}
	return h >= startHour || h < endHour
}

func detect(cfg engineConfig, st *vehicleState, s snapshot, loc *time.Location) []detectedEvent {
	if !s.ts.After(st.watermark) {
		return nil
	}
	dt := s.ts.Sub(st.watermark)
	gap := dt > cfg.gapTolerance

	var events []detectedEvent
	if gap || !st.hasLast {
		st.speedStreak = 0
		st.speeding = false
		st.idleStart = time.Time{}
	} else if cfg.speedLimitKmh > 0 {
		events = append(events, evalSpeeding(cfg, st, s, dt)...)
		events = append(events, evalHarshBraking(cfg, st, s, dt)...)
		events = append(events, evalHarshAccel(cfg, st, s, dt)...)
		events = append(events, evalIdling(cfg, st, s, dt)...)
	}
	events = append(events, evalNightDriving(cfg, st, s, loc)...)

	if s.driverID != "" {
		st.driverID = s.driverID
	}
	st.lastSpeed = s.speed
	st.hasLast = true
	st.watermark = s.ts
	return events
}

func evalSpeeding(cfg engineConfig, st *vehicleState, s snapshot, _ time.Duration) []detectedEvent {
	over := s.speed > cfg.speedLimitKmh
	if !over {
		st.speedStreak = 0
		st.speeding = false
		return nil
	}
	st.speedStreak++
	if st.speedStreak < 2 || st.speeding {
		return nil
	}
	st.speeding = true
	ratio := (s.speed - cfg.speedLimitKmh) / cfg.speedLimitKmh
	sev := "low"
	switch {
	case ratio >= 0.25:
		sev = "high"
	case ratio >= 0.10:
		sev = "medium"
	}
	ev := detectedEvent{
		eventType:  EventSpeeding,
		severity:   sev,
		weight:     cfg.weightSpeeding,
		occurredAt: s.ts,
		metadata:   fmt.Sprintf(`{"speed_kmh":%.1f,"limit_kmh":%.1f}`, s.speed, cfg.speedLimitKmh),
	}
	st.markEvent(EventSpeeding, s.ts)
	return []detectedEvent{ev}
}

func evalHarshBraking(cfg engineConfig, st *vehicleState, s snapshot, dt time.Duration) []detectedEvent {
	if cfg.brakeRateKmhS >= 0 {
		return nil
	}
	dts := dt.Seconds()
	if dts <= 0 || dts > cfg.accelMaxDTSecs {
		return nil
	}
	if st.lastSpeed < cfg.minSpeedKmh {
		return nil
	}
	rate := (s.speed - st.lastSpeed) / dts
	if rate > cfg.brakeRateKmhS {
		return nil
	}
	if st.onCooldown(EventHarshBraking, s.ts, time.Duration(cfg.harshCooldownSec)*time.Second) {
		return nil
	}
	sev := "medium"
	if rate <= cfg.brakeRateKmhS*1.5 {
		sev = "high"
	}
	st.markEvent(EventHarshBraking, s.ts)
	return []detectedEvent{{
		eventType:  EventHarshBraking,
		severity:   sev,
		weight:     cfg.weightHarshBraking,
		occurredAt: s.ts,
		metadata: fmt.Sprintf(`{"speed_kmh":%.1f,"prev_speed_kmh":%.1f,"rate_kmh_s":%.2f,"dt_s":%.1f}`,
			s.speed, st.lastSpeed, rate, dts),
	}}
}

func evalHarshAccel(cfg engineConfig, st *vehicleState, s snapshot, dt time.Duration) []detectedEvent {
	if cfg.accelRateKmhS <= 0 {
		return nil
	}
	dts := dt.Seconds()
	if dts <= 0 || dts > cfg.accelMaxDTSecs {
		return nil
	}
	if st.lastSpeed < cfg.minSpeedKmh || s.speed < cfg.minSpeedKmh {
		return nil
	}
	rate := (s.speed - st.lastSpeed) / dts
	if rate < cfg.accelRateKmhS {
		return nil
	}
	if st.onCooldown(EventHarshAccel, s.ts, time.Duration(cfg.harshCooldownSec)*time.Second) {
		return nil
	}
	sev := "medium"
	if rate >= cfg.accelRateKmhS*1.5 {
		sev = "high"
	}
	st.markEvent(EventHarshAccel, s.ts)
	return []detectedEvent{{
		eventType:  EventHarshAccel,
		severity:   sev,
		weight:     cfg.weightHarshAccel,
		occurredAt: s.ts,
		metadata: fmt.Sprintf(`{"speed_kmh":%.1f,"prev_speed_kmh":%.1f,"rate_kmh_s":%.2f,"dt_s":%.1f}`,
			s.speed, st.lastSpeed, rate, dts),
	}}
}

func evalIdling(cfg engineConfig, st *vehicleState, s snapshot, _ time.Duration) []detectedEvent {
	idle := s.ignition != nil && *s.ignition && s.speed < cfg.idleMaxSpeedKmh
	if !idle {
		st.idleStart = time.Time{}
		return nil
	}
	if st.idleStart.IsZero() {
		st.idleStart = s.ts
		return nil
	}
	minutes := s.ts.Sub(st.idleStart).Minutes()
	if minutes < cfg.idleMinutes || st.onCooldown(EventIdling, s.ts, time.Duration(cfg.idleMinutes)*time.Minute) {
		return nil
	}
	st.markEvent(EventIdling, s.ts)
	return []detectedEvent{{
		eventType:  EventIdling,
		severity:   "low",
		weight:     cfg.weightIdling,
		occurredAt: s.ts,
		metadata: fmt.Sprintf(`{"idle_minutes":%.0f,"speed_kmh":%.1f}`,
			minutes, s.speed),
	}}
}

func evalNightDriving(cfg engineConfig, st *vehicleState, s snapshot, loc *time.Location) []detectedEvent {
	if !isNight(s.ts, cfg.nightStartHour, cfg.nightEndHour, loc) {
		return nil
	}
	if s.speed < cfg.nightMinSpeedKmh {
		return nil
	}
	cooldown := time.Duration(cfg.nightCooldownMin) * time.Minute
	if st.onCooldown(EventNightDriving, s.ts, cooldown) {
		return nil
	}
	st.markEvent(EventNightDriving, s.ts)
	return []detectedEvent{{
		eventType:  EventNightDriving,
		severity:   "medium",
		weight:     cfg.weightNightDriving,
		occurredAt: s.ts,
		metadata: fmt.Sprintf(`{"speed_kmh":%.1f,"local_hour":%d}`,
			s.speed, s.ts.In(loc).Hour()),
	}}
}
