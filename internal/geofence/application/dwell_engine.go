// Package application contains the geofence dwell state machine (Spec 02 §3).
package application

import (
	"time"

	"transport-app/internal/geofence/domain"
)

// EngineConfig tunes the dwell state machine.
type EngineConfig struct {
	Debounce         time.Duration // continuous fixes inside/outside before confirming
	BufferMetres     float64       // expand zones for the entry test
	HysteresisMetres float64       // contract zones for the exit test
}

// DefaultEngineConfig matches Spec 02 §3 constants.
func DefaultEngineConfig() EngineConfig {
	return EngineConfig{
		Debounce:         DefaultDwellDebounce,
		BufferMetres:     DefaultBufferMetres,
		HysteresisMetres: DefaultHysteresisMetres,
	}
}

// ZoneEvent is a durable outcome of evaluating one fix.
type ZoneEvent struct {
	EventType string // entering | leaving | breach
	Zone      domain.Geofence
	Lat       float64
	Lng       float64
	At        time.Time
	Details   string
}

// DwellEngine is the pure, stateless 4-state machine
// (outside → entering → inside → leaving). Evaluation never touches the DB.
type DwellEngine struct {
	config EngineConfig
}

// NewDwellEngine constructs a DwellEngine.
func NewDwellEngine(cfg EngineConfig) *DwellEngine {
	return &DwellEngine{config: cfg}
}

// Evaluate applies one GPS fix to the current engine state and returns the
// next state plus any zone events to persist. `zones` must be ordered by
// priority (descending); the first zone containing the fix wins. The exit
// test runs against the zone recorded in the current state.
func (e *DwellEngine) Evaluate(current domain.EngineState, fix domain.Fix, zones []domain.Geofence) (domain.EngineState, []ZoneEvent) {
	now := fix.Timestamp
	next := current
	next.LastFixAt = now
	next.LastLat = fix.Latitude
	next.LastLng = fix.Longitude
	if fix.TripID != nil {
		next.TripID = fix.TripID
	}

	var events []ZoneEvent
	entryZone := bestZoneFor(zones, fix.Latitude, fix.Longitude, e.config.BufferMetres)

	switch current.State {
	case domain.StateOutside:
		if entryZone != nil {
			// Entry probe: expanded entry test, no debounce yet.
			next.State = domain.StateEntering
			t := now
			next.ZoneEnteredAt = &t
			next.GeofenceID = &entryZone.ID
			next.ZoneKind = &entryZone.Kind
		}

	case domain.StateEntering:
		if entryZone == nil {
			// Jitter: a single miss reverts the entry probe.
			next.State = domain.StateOutside
			next.GeofenceID = nil
			next.ZoneKind = nil
			next.ZoneEnteredAt = nil
			break
		}
		if entryZone.ID != *current.GeofenceID {
			// Moved into a different zone mid-probe: restart the timer.
			t := now
			next.ZoneEnteredAt = &t
			next.GeofenceID = &entryZone.ID
			next.ZoneKind = &entryZone.Kind
			break
		}
		if current.ZoneEnteredAt == nil || now.Sub(*current.ZoneEnteredAt) >= e.config.Debounce {
			// Debounce satisfied: confirm the entry.
			next.State = domain.StateInside
			t := now
			next.ConfirmedAt = &t
			events = append(events, ZoneEvent{
				EventType: domain.EventEntering,
				Zone:      *entryZone,
				Lat:       fix.Latitude, Lng: fix.Longitude, At: now,
			})
			if entryZone.Kind == domain.KindRestricted || entryZone.Kind == domain.KindNoEntry {
				events = append(events, ZoneEvent{
					EventType: domain.EventBreach,
					Zone:      *entryZone,
					Lat:       fix.Latitude, Lng: fix.Longitude, At: now,
					Details: "vehicle entered " + entryZone.Kind + " zone",
				})
			}
		}

	case domain.StateInside:
		var cur *domain.Geofence
		if current.GeofenceID != nil {
			cur = zoneByID(zones, current.GeofenceID)
		} else {
			cur = entryZone
		}
		if cur == nil {
			// Previous zone is no longer active
			if entryZone != nil {
				next.State = domain.StateEntering
				t := now
				next.ZoneEnteredAt = &t
				next.GeofenceID = &entryZone.ID
				next.ZoneKind = &entryZone.Kind
				next.ConfirmedAt = nil
				next.ExitStartedAt = nil
			} else {
				next.State = domain.StateOutside
				next.GeofenceID = nil
				next.ZoneKind = nil
				next.ZoneEnteredAt = nil
				next.ConfirmedAt = nil
				next.ExitStartedAt = nil
			}
			break
		}
		if current.GeofenceID == nil {
			next.GeofenceID = &cur.ID
			next.ZoneKind = &cur.Kind
		}
		// Exit test: zone contracted by hysteresis.
		if !cur.ContainsExit(fix.Latitude, fix.Longitude, e.config.HysteresisMetres) {
			next.State = domain.StateLeaving
			t := now
			next.ExitStartedAt = &t
		}

	case domain.StateLeaving:
		if entryZone != nil {
			// Jitter or transition to new zone
			if current.GeofenceID == nil || entryZone.ID != *current.GeofenceID {
				next.State = domain.StateEntering
				t := now
				next.ZoneEnteredAt = &t
				next.GeofenceID = &entryZone.ID
				next.ZoneKind = &entryZone.Kind
				next.ExitStartedAt = nil
				break
			}
			next.State = domain.StateInside
			next.ExitStartedAt = nil
			break
		}
		if current.ExitStartedAt == nil || now.Sub(*current.ExitStartedAt) >= e.config.Debounce {
			// Debounce satisfied: confirm the exit.
			next.State = domain.StateOutside
			next.GeofenceID = nil
			next.ZoneKind = nil
			next.ZoneEnteredAt = nil
			next.ConfirmedAt = nil
			next.ExitStartedAt = nil
			if cur := zoneByID(zones, current.GeofenceID); cur != nil {
				events = append(events, ZoneEvent{
					EventType: domain.EventLeaving,
					Zone:      *cur,
					Lat:       fix.Latitude, Lng: fix.Longitude, At: now,
				})
			}
		}
	}

	return next, events
}

// bestZoneFor returns the highest-priority zone that contains the point
// under the entry test (expanded by buffer).
func bestZoneFor(zones []domain.Geofence, lat, lng, buffer float64) *domain.Geofence {
	for i := range zones {
		if zones[i].ContainsEntry(lat, lng, buffer) {
			return &zones[i]
		}
	}
	return nil
}

// zoneByID looks up a zone in the loaded list by ID.
func zoneByID(zones []domain.Geofence, id *string) *domain.Geofence {
	if id == nil {
		return nil
	}
	for i := range zones {
		if zones[i].ID == *id {
			return &zones[i]
		}
	}
	return nil
}
