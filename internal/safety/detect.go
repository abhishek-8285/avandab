package safety

import (
	"fmt"
	"math"
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
	lat       float64
	lng       float64
	accuracy  float64
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
	lastLat     float64
	lastLng     float64
	hasLastPos  bool
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
	if loc == nil {
		loc = time.UTC
	}
	h := float64(ts.In(loc).Hour()) + float64(ts.In(loc).Minute())/60.0
	if startHour <= endHour {
		return h >= startHour && h < endHour
	}
	return h >= startHour || h < endHour
}

// haversineKM computes great-circle distance between two geographic coordinates in kilometers.
func haversineKM(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusKM = 6371.0
	dLat := (lat2 - lat1) * (math.Pi / 180.0)
	dLon := (lon2 - lon1) * (math.Pi / 180.0)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*(math.Pi/180.0))*math.Cos(lat2*(math.Pi/180.0))*
			math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusKM * c
}

func detect(policy SafetyPolicy, st *vehicleState, s snapshot, loc *time.Location) []detectedEvent {
	if !s.ts.After(st.watermark) {
		return nil
	}

	// 1. False Positive Gate: Reject noisy/poor GPS accuracy
	if s.accuracy > 0 && policy.MaxAccuracyMeters > 0 && s.accuracy > policy.MaxAccuracyMeters {
		return nil
	}

	dt := s.ts.Sub(st.watermark)
	gap := dt > policy.GapTolerance

	// 2. False Positive Gate: Detect impossible GPS teleportation jumps
	if st.hasLastPos && s.lat != 0 && s.lng != 0 && st.lastLat != 0 && st.lastLng != 0 && dt.Seconds() > 0 {
		distKM := haversineKM(st.lastLat, st.lastLng, s.lat, s.lng)
		impliedSpeedKmh := distKM / (dt.Seconds() / 3600.0)
		if policy.MaxFeasibleSpeedKmh > 0 && impliedSpeedKmh > policy.MaxFeasibleSpeedKmh {
			// Impossible GPS jump detected: update position & watermark but skip motion anomaly evaluation
			st.lastLat = s.lat
			st.lastLng = s.lng
			st.lastSpeed = s.speed
			st.speedStreak = 0
			st.speeding = false
			st.watermark = s.ts
			return nil
		}
	}

	var events []detectedEvent
	if gap || !st.hasLast {
		st.speedStreak = 0
		st.speeding = false
		st.idleStart = time.Time{}
	} else if policy.SpeedLimitKmh > 0 {
		events = append(events, evalSpeeding(policy, st, s, dt)...)
		events = append(events, evalHarshBraking(policy, st, s, dt)...)
		events = append(events, evalHarshAccel(policy, st, s, dt)...)
		events = append(events, evalIdling(policy, st, s, dt)...)
	}
	events = append(events, evalNightDriving(policy, st, s, loc)...)

	if s.driverID != "" {
		st.driverID = s.driverID
	}
	st.lastSpeed = s.speed
	if s.lat != 0 && s.lng != 0 {
		st.lastLat = s.lat
		st.lastLng = s.lng
		st.hasLastPos = true
	}
	st.hasLast = true
	st.watermark = s.ts
	return events
}

func evalSpeeding(policy SafetyPolicy, st *vehicleState, s snapshot, _ time.Duration) []detectedEvent {
	over := s.speed > policy.SpeedLimitKmh
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
	ratio := (s.speed - policy.SpeedLimitKmh) / policy.SpeedLimitKmh
	sev := "low"
	switch {
	case ratio >= 0.25:
		sev = "high"
	case ratio >= 0.10:
		sev = "medium"
	}
	weight := policy.Weights[EventSpeeding]
	if weight <= 0 {
		weight = 8.0
	}
	ev := detectedEvent{
		eventType:  EventSpeeding,
		severity:   sev,
		weight:     weight,
		occurredAt: s.ts,
		metadata:   fmt.Sprintf(`{"speed_kmh":%.1f,"limit_kmh":%.1f}`, s.speed, policy.SpeedLimitKmh),
	}
	st.markEvent(EventSpeeding, s.ts)
	return []detectedEvent{ev}
}

func evalHarshBraking(policy SafetyPolicy, st *vehicleState, s snapshot, dt time.Duration) []detectedEvent {
	if policy.BrakeRateKmhS >= 0 {
		return nil
	}
	dts := dt.Seconds()
	if dts <= 0 || dts > policy.AccelMaxDTSecs {
		return nil
	}
	if st.lastSpeed < policy.MinSpeedKmh {
		return nil
	}
	rate := (s.speed - st.lastSpeed) / dts
	if rate > policy.BrakeRateKmhS {
		return nil
	}
	if st.onCooldown(EventHarshBraking, s.ts, time.Duration(policy.HarshCooldownSec)*time.Second) {
		return nil
	}
	sev := "medium"
	if rate <= policy.BrakeRateKmhS*1.5 {
		sev = "high"
	}
	weight := policy.Weights[EventHarshBraking]
	if weight <= 0 {
		weight = 6.0
	}
	st.markEvent(EventHarshBraking, s.ts)
	return []detectedEvent{{
		eventType:  EventHarshBraking,
		severity:   sev,
		weight:     weight,
		occurredAt: s.ts,
		metadata: fmt.Sprintf(`{"speed_kmh":%.1f,"prev_speed_kmh":%.1f,"rate_kmh_s":%.2f,"dt_s":%.1f}`,
			s.speed, st.lastSpeed, rate, dts),
	}}
}

func evalHarshAccel(policy SafetyPolicy, st *vehicleState, s snapshot, dt time.Duration) []detectedEvent {
	if policy.AccelRateKmhS <= 0 {
		return nil
	}
	dts := dt.Seconds()
	if dts <= 0 || dts > policy.AccelMaxDTSecs {
		return nil
	}
	if st.lastSpeed < policy.MinSpeedKmh || s.speed < policy.MinSpeedKmh {
		return nil
	}
	rate := (s.speed - st.lastSpeed) / dts
	if rate < policy.AccelRateKmhS {
		return nil
	}
	if st.onCooldown(EventHarshAccel, s.ts, time.Duration(policy.HarshCooldownSec)*time.Second) {
		return nil
	}
	sev := "medium"
	if rate >= policy.AccelRateKmhS*1.5 {
		sev = "high"
	}
	weight := policy.Weights[EventHarshAccel]
	if weight <= 0 {
		weight = 6.0
	}
	st.markEvent(EventHarshAccel, s.ts)
	return []detectedEvent{{
		eventType:  EventHarshAccel,
		severity:   sev,
		weight:     weight,
		occurredAt: s.ts,
		metadata: fmt.Sprintf(`{"speed_kmh":%.1f,"prev_speed_kmh":%.1f,"rate_kmh_s":%.2f,"dt_s":%.1f}`,
			s.speed, st.lastSpeed, rate, dts),
	}}
}

func evalIdling(policy SafetyPolicy, st *vehicleState, s snapshot, _ time.Duration) []detectedEvent {
	idle := s.ignition != nil && *s.ignition && s.speed < policy.IdleMaxSpeedKmh
	if !idle {
		st.idleStart = time.Time{}
		return nil
	}
	if st.idleStart.IsZero() {
		st.idleStart = s.ts
		return nil
	}
	minutes := s.ts.Sub(st.idleStart).Minutes()
	if minutes < policy.IdleMinutes || st.onCooldown(EventIdling, s.ts, time.Duration(policy.IdleMinutes)*time.Minute) {
		return nil
	}
	weight := policy.Weights[EventIdling]
	if weight <= 0 {
		weight = 3.0
	}
	st.markEvent(EventIdling, s.ts)
	return []detectedEvent{{
		eventType:  EventIdling,
		severity:   "low",
		weight:     weight,
		occurredAt: s.ts,
		metadata: fmt.Sprintf(`{"idle_minutes":%.0f,"speed_kmh":%.1f}`,
			minutes, s.speed),
	}}
}

func evalNightDriving(policy SafetyPolicy, st *vehicleState, s snapshot, loc *time.Location) []detectedEvent {
	if !isNight(s.ts, policy.NightStartHour, policy.NightEndHour, loc) {
		return nil
	}
	if s.speed < policy.NightMinSpeedKmh {
		return nil
	}
	cooldown := time.Duration(policy.NightCooldownMin) * time.Minute
	if st.onCooldown(EventNightDriving, s.ts, cooldown) {
		return nil
	}
	weight := policy.Weights[EventNightDriving]
	if weight <= 0 {
		weight = 2.0
	}
	st.markEvent(EventNightDriving, s.ts)
	return []detectedEvent{{
		eventType:  EventNightDriving,
		severity:   "medium",
		weight:     weight,
		occurredAt: s.ts,
		metadata: fmt.Sprintf(`{"speed_kmh":%.1f,"local_hour":%d}`,
			s.speed, s.ts.In(loc).Hour()),
	}}
}
