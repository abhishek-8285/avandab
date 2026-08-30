package deviation

import (
	"time"

	geodomain "transport-app/internal/geofence/domain"
)

// DeviationState represents the state of a trip's route compliance.
type DeviationState string

const (
	StateOnRoute         DeviationState = "ON_ROUTE"
	StateDeviating       DeviationState = "DEVIATING"
	StateSustained       DeviationState = "SUSTAINED_DEVIATION"
	StateAlerted         DeviationState = "ALERTED"
	StateReturnedToRoute DeviationState = "RETURNED_TO_ROUTE"
)

// TripDeviationTracker maintains the in-memory state of an active trip.
type TripDeviationTracker struct {
	TripID           string
	VehicleID        string
	DriverID         string
	State            DeviationState
	DeviationStart   time.Time
	StreakCount      int
	LastAlertAt      time.Time
	MaxDeviationDist float64
	LastLat          float64
	LastLng          float64
	HasLastFix       bool
	LastFixTime      time.Time
}

// NewTripDeviationTracker constructs a tracker for a trip.
func NewTripDeviationTracker(tripID, vehicleID, driverID string) *TripDeviationTracker {
	return &TripDeviationTracker{
		TripID:    tripID,
		VehicleID: vehicleID,
		DriverID:  driverID,
		State:     StateOnRoute,
	}
}

// Step evaluates a new telemetry point against the state machine.
// Returns (eventToEmit, ok) where ok is true only if a new deviation alert should be dispatched.
func (t *TripDeviationTracker) Step(policy DeviationPolicy, corridor *RouteCorridor, lat, lng, speed, accuracy float64, ts time.Time) (float64, DeviationState, bool) {
	if corridor == nil || len(corridor.Waypoints) < 2 {
		return 0, t.State, false
	}

	// 1. False positive gate: Reject poor GPS accuracy fixes
	if accuracy > 0 && policy.MaxAccuracyMeters > 0 && accuracy > policy.MaxAccuracyMeters {
		return 0, t.State, false
	}

	// 2. False positive gate: Detect impossible GPS jump (teleportation anomaly)
	if t.HasLastFix && t.LastLat != 0 && t.LastLng != 0 && !t.LastFixTime.IsZero() {
		dtSec := ts.Sub(t.LastFixTime).Seconds()
		if dtSec > 0 {
			distM := geodomain.Haversine(t.LastLat, t.LastLng, lat, lng)
			speedKmh := (distM / 1000.0) / (dtSec / 3600.0)
			if policy.MaxFeasibleSpeedKmh > 0 && speedKmh > policy.MaxFeasibleSpeedKmh {
				// Impossible GPS jump detected: discard corrupted point completely without polluting last known valid fix
				return 0, t.State, false
			}
		}
	}

	distM := corridor.DistanceToPoint(lat, lng)

	// Update movement position
	t.LastLat = lat
	t.LastLng = lng
	t.HasLastFix = true
	t.LastFixTime = ts

	shouldAlert := false

	switch t.State {
	case StateOnRoute, StateReturnedToRoute:
		if distM > policy.MaxDistanceMeters {
			// Vehicle moved outside planned route corridor
			t.State = StateDeviating
			t.DeviationStart = ts
			t.StreakCount = 1
			t.MaxDeviationDist = distM
		} else {
			t.State = StateOnRoute
		}

	case StateDeviating:
		if distM <= policy.ReturnDistanceMeters {
			// Transient spike or quick return
			t.State = StateOnRoute
			t.StreakCount = 0
			t.DeviationStart = time.Time{}
		} else {
			t.StreakCount++
			if distM > t.MaxDeviationDist {
				t.MaxDeviationDist = distM
			}
			elapsedSec := ts.Sub(t.DeviationStart).Seconds()
			if t.StreakCount >= policy.MinStreakCount && elapsedSec >= policy.SustainedDurationSec {
				t.State = StateAlerted
				t.LastAlertAt = ts
				shouldAlert = true
			}
		}

	case StateAlerted:
		if distM <= policy.ReturnDistanceMeters {
			// Vehicle returned to route
			t.State = StateReturnedToRoute
			t.StreakCount = 0
			t.DeviationStart = time.Time{}
		} else {
			if distM > t.MaxDeviationDist {
				t.MaxDeviationDist = distM
			}
			// Periodic cooldown re-alerting if enabled and outside cooldown window
			if policy.CooldownDuration > 0 && !t.LastAlertAt.IsZero() && ts.Sub(t.LastAlertAt) >= policy.CooldownDuration {
				t.LastAlertAt = ts
				shouldAlert = true
			}
		}
	}

	return distM, t.State, shouldAlert
}
