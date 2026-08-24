package eta

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sync"
	"time"
)

// EtaResult is the output of the hybrid ETA calculation (Spec 04 §5).
type EtaResult struct {
	ArrivalAt   time.Time `json:"arrival_at"`
	EtaMin      time.Time `json:"eta_min"`
	EtaMax      time.Time `json:"eta_max"`
	Method      string    `json:"eta_method"` // "hybrid" | "telemetry" | "scheduled" | "telemetry_time_prop"
	Confidence  string    `json:"confidence"` // "high" | "medium" | "low"
	RemainingKM float64   `json:"remaining_km"`
	AvgSpeed    float64   `json:"avg_speed_kmh"`
}

// EtaService computes hybrid ETA for active trips (Spec 04 §5).
type EtaService struct {
	db              *sql.DB
	staleMin        int // ETA_STALE_MIN (default 15)
	windowMin       int // ETA_WINDOW_MIN (default 30)
	guardMaxRegress int // ETA_GUARD_MAX_REGRESS_MIN (default 5)

	// In-memory monotonic guard state (per-trip last arrival).
	// Single-process model (Spec 04 §1.2).
	mu      sync.RWMutex
	lastETA map[string]time.Time
}

// NewEtaService creates a new EtaService instance with configured parameters.
func NewEtaService(db *sql.DB, staleMin, windowMin, guardMaxRegress int) *EtaService {
	if staleMin <= 0 {
		staleMin = 15
	}
	if windowMin <= 0 {
		windowMin = 30
	}
	if guardMaxRegress <= 0 {
		guardMaxRegress = 5
	}
	return &EtaService{
		db:              db,
		staleMin:        staleMin,
		windowMin:       windowMin,
		guardMaxRegress: guardMaxRegress,
		lastETA:         make(map[string]time.Time),
	}
}

type tripData struct {
	TripID         string
	Status         string
	VehicleID      string
	RouteDistance  float64 // km
	EstimatedHours float64
	StartedAt      *time.Time
	DepartureTime  *time.Time
	ArrivalTime    *time.Time
}

type snapshot struct {
	Timestamp time.Time
	Speed     float64
	Odometer  *float64
	VehicleID string
}

func parseDBTime(ns sql.NullString, nt sql.NullTime) *time.Time {
	if nt.Valid {
		t := nt.Time.UTC()
		return &t
	}
	if ns.Valid && ns.String != "" {
		formats := []string{
			time.RFC3339,
			"2006-01-02 15:04:05",
			"2006-01-02T15:04:05Z07:00",
			"2006-01-02 15:04:05-07:00",
			"2006-01-02",
		}
		for _, f := range formats {
			if t, err := time.Parse(f, ns.String); err == nil {
				tUTC := t.UTC()
				return &tUTC
			}
		}
	}
	return nil
}

func (s *EtaService) loadTrip(ctx context.Context, tripID string) (*tripData, error) {
	var t tripData
	var rDist, rHours sql.NullFloat64
	var vID sql.NullString
	var sAtS, dTimeS, aTimeS sql.NullString
	var sAtT, dTimeT, aTimeT sql.NullTime
	var srcLat, srcLng, dstLat, dstLng sql.NullFloat64

	err := s.db.QueryRowContext(ctx, `
		SELECT t.id, t.status, t.vehicle_id, COALESCE(r.distance, 0), r.estimated_hours,
		       t.started_at, t.departure_time, t.arrival_time,
		       t.started_at, t.departure_time, t.arrival_time,
		       rl.source_lat, rl.source_lng, rl.dest_lat, rl.dest_lng
		FROM trips t
		LEFT JOIN routes r ON t.route_id = r.id
		LEFT JOIN route_locations rl ON rl.route_id = r.id
		WHERE t.id = ?`, tripID).Scan(
		&t.TripID, &t.Status, &vID, &rDist, &rHours,
		&sAtT, &dTimeT, &aTimeT,
		&sAtS, &dTimeS, &aTimeS,
		&srcLat, &srcLng, &dstLat, &dstLng,
	)
	if err != nil {
		return nil, err
	}

	if vID.Valid {
		t.VehicleID = vID.String
	}
	if rDist.Valid {
		t.RouteDistance = rDist.Float64
	}
	if rHours.Valid {
		t.EstimatedHours = rHours.Float64
	}
	if t.RouteDistance <= 0 && srcLat.Valid && srcLng.Valid && dstLat.Valid && dstLng.Valid {
		t.RouteDistance = haversineKm(srcLat.Float64, srcLng.Float64, dstLat.Float64, dstLng.Float64) * roadFactor
	}

	t.StartedAt = parseDBTime(sAtS, sAtT)
	t.DepartureTime = parseDBTime(dTimeS, dTimeT)
	t.ArrivalTime = parseDBTime(aTimeS, aTimeT)

	return &t, nil
}

func (s *EtaService) loadLatestSnapshot(ctx context.Context, tripID, vehicleID string) (*snapshot, error) {
	var sn snapshot
	var speed, odo sql.NullFloat64
	var tsT sql.NullTime
	var tsS sql.NullString
	var vID sql.NullString

	err := s.db.QueryRowContext(ctx, `
		SELECT timestamp, speed, odometer, vehicle_id, timestamp
		FROM telemetry_snapshots
		WHERE (trip_id = ? OR (vehicle_id = ? AND vehicle_id != ''))
		  AND latitude IS NOT NULL AND longitude IS NOT NULL
		ORDER BY timestamp DESC LIMIT 1`, tripID, vehicleID).Scan(
		&tsT, &speed, &odo, &vID, &tsS,
	)
	if err != nil {
		return nil, err
	}

	t := parseDBTime(tsS, tsT)
	if t != nil {
		sn.Timestamp = *t
	}
	if speed.Valid {
		sn.Speed = speed.Float64
	}
	if odo.Valid {
		sn.Odometer = &odo.Float64
	}
	if vID.Valid {
		sn.VehicleID = vID.String
	}

	return &sn, nil
}

func (s *EtaService) rollingAvgSpeed(ctx context.Context, tripID string, vehicleID string) (float64, int, error) {
	windowStart := time.Now().UTC().Add(-time.Duration(s.windowMin) * time.Minute).Format("2006-01-02 15:04:05")
	rows, err := s.db.QueryContext(ctx, `
		SELECT speed FROM telemetry_snapshots
		WHERE (trip_id = ? OR (vehicle_id = ? AND vehicle_id != ''))
		  AND timestamp >= ? AND speed > 0
		ORDER BY timestamp DESC`, tripID, vehicleID, windowStart)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()

	var sum float64
	var count int
	for rows.Next() {
		var speed float64
		if err := rows.Scan(&speed); err != nil {
			continue
		}
		sum += speed
		count++
	}

	if count == 0 {
		return 0, 0, fmt.Errorf("no speed samples")
	}
	return sum / float64(count), count, nil
}

func (s *EtaService) remainingDistance(ctx context.Context, trip *tripData, latest *snapshot) (float64, string, error) {
	// 1. Odometer-delta method
	if latest.Odometer != nil {
		startRef := trip.StartedAt
		if startRef == nil {
			startRef = trip.DepartureTime
		}
		var odomStart float64
		var err error
		if startRef != nil {
			startStr := startRef.Format("2006-01-02 15:04:05")
			err = s.db.QueryRowContext(ctx, `
				SELECT odometer FROM telemetry_snapshots
				WHERE (trip_id = ? OR (vehicle_id = ? AND vehicle_id != ''))
				  AND timestamp >= ? AND odometer IS NOT NULL
				ORDER BY timestamp ASC LIMIT 1`, trip.TripID, trip.VehicleID, startStr).Scan(&odomStart)
		} else {
			err = s.db.QueryRowContext(ctx, `
				SELECT odometer FROM telemetry_snapshots
				WHERE (trip_id = ? OR (vehicle_id = ? AND vehicle_id != ''))
				  AND odometer IS NOT NULL
				ORDER BY timestamp ASC LIMIT 1`, trip.TripID, trip.VehicleID).Scan(&odomStart)
		}

		if err == nil && odomStart > 0 {
			distanceTravelled := *latest.Odometer - odomStart
			if distanceTravelled >= 0 && trip.RouteDistance > 0 {
				remaining := math.Max(0, trip.RouteDistance-distanceTravelled)
				return remaining, "odometer", nil
			}
		}
	}

	// 2. Fallback: time-proportional
	if trip.DepartureTime != nil && trip.EstimatedHours > 0 && trip.RouteDistance > 0 {
		elapsed := time.Since(*trip.DepartureTime).Hours()
		if elapsed > 0 {
			progress := math.Min(1.0, math.Max(0.0, elapsed/trip.EstimatedHours))
			remaining := trip.RouteDistance * (1 - progress)
			return remaining, "telemetry_time_prop", nil
		}
	}

	// 3. Fallback: full route distance
	return trip.RouteDistance, "scheduled", nil
}

func (s *EtaService) scheduledFallback(ctx context.Context, trip *tripData, tripID string, reason string) (EtaResult, error) {
	var arrivalAt time.Time
	if trip.ArrivalTime != nil {
		arrivalAt = *trip.ArrivalTime
	} else if trip.DepartureTime != nil && trip.EstimatedHours > 0 {
		arrivalAt = trip.DepartureTime.Add(time.Duration(trip.EstimatedHours * float64(time.Hour)))
	} else {
		return EtaResult{}, fmt.Errorf("eta: no arrival time or departure time available for trip %s", tripID)
	}

	// Apply monotonic guard even for scheduled fallback
	arrivalAt = s.applyMonotonicGuard(ctx, tripID, arrivalAt)

	// Audit log for fallback switch
	s.writeAuditLog(ctx, tripID, "eta_fallback", reason)

	return EtaResult{
		ArrivalAt:   arrivalAt,
		EtaMin:      arrivalAt.Add(-15 * time.Minute),
		EtaMax:      arrivalAt.Add(15 * time.Minute),
		Method:      "scheduled",
		Confidence:  "low",
		RemainingKM: trip.RouteDistance,
	}, nil
}

func (s *EtaService) applyMonotonicGuard(ctx context.Context, tripID string, newArrival time.Time) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()

	last, exists := s.lastETA[tripID]
	if !exists {
		s.lastETA[tripID] = newArrival
		return newArrival
	}

	// ETA must not jump backward (earlier arrival) more than guardMaxRegress minutes
	maxRegress := time.Duration(s.guardMaxRegress) * time.Minute
	earliestAllowed := last.Add(-maxRegress)
	if newArrival.Before(earliestAllowed) {
		clampedArrival := earliestAllowed
		s.writeAuditLog(ctx, tripID, "eta_guard",
			fmt.Sprintf("clamped from %s to %s", newArrival.Format(time.RFC3339), clampedArrival.Format(time.RFC3339)))
		s.lastETA[tripID] = clampedArrival
		return clampedArrival
	}

	s.lastETA[tripID] = newArrival
	return newArrival
}

func (s *EtaService) writeAuditLog(ctx context.Context, tripID string, action string, reason string) {
	if s.db == nil {
		return
	}
	auditID := fmt.Sprintf("eta-%d", time.Now().UnixNano())
	newValues := fmt.Sprintf(`{"reason":"%s"}`, reason)
	_, _ = s.db.ExecContext(ctx, `
		INSERT INTO audit_logs (id, action, table_name, record_id, new_values, created_at)
		VALUES (?, ?, 'trips', ?, ?, CURRENT_TIMESTAMP)`,
		auditID, action, tripID, newValues,
	)
}

// Calculate computes the hybrid ETA for a trip (Spec 04 §5).
// Pure read path — no UoW needed.
func (s *EtaService) Calculate(ctx context.Context, tripID string) (EtaResult, error) {
	// Step 1: Load trip + route
	trip, err := s.loadTrip(ctx, tripID)
	if err != nil {
		return EtaResult{}, fmt.Errorf("eta: trip not found: %w", err)
	}

	// Only calculate ETA for active trip phases
	if trip.Status != "started" && trip.Status != "reached_pickup" && trip.Status != "in_transit" && trip.Status != "delivered" {
		return EtaResult{}, fmt.Errorf("eta: trip %s is in %s phase (not active)", tripID, trip.Status)
	}

	// Step 2: Freshness gate
	latestSnapshot, err := s.loadLatestSnapshot(ctx, tripID, trip.VehicleID)
	if err != nil || latestSnapshot == nil || latestSnapshot.Timestamp.IsZero() {
		return s.scheduledFallback(ctx, trip, tripID, "no_telemetry")
	}

	age := time.Since(latestSnapshot.Timestamp)
	if age > time.Duration(s.staleMin)*time.Minute {
		return s.scheduledFallback(ctx, trip, tripID, "stale_telemetry")
	}

	// Step 3: Rolling average speed
	avgSpeed, sampleCount, err := s.rollingAvgSpeed(ctx, tripID, trip.VehicleID)
	if err != nil || sampleCount < 3 || avgSpeed <= 0 {
		return s.scheduledFallback(ctx, trip, tripID, "insufficient_samples")
	}

	// Step 4: Remaining distance
	remainingKM, distMethod, err := s.remainingDistance(ctx, trip, latestSnapshot)
	if err != nil {
		return s.scheduledFallback(ctx, trip, tripID, "distance_error")
	}

	// Step 5: Compute components
	etaTelemetry := remainingKM / avgSpeed // in hours
	var etaScheduled float64
	if trip.RouteDistance > 0 && trip.EstimatedHours > 0 {
		etaScheduled = trip.EstimatedHours * (remainingKM / trip.RouteDistance) // in hours
	}

	// Step 6: Hybrid blend (0.7 telemetry + 0.3 scheduled)
	var etaHours float64
	var etaMethod string
	if avgSpeed > 0 && etaScheduled > 0 {
		etaHours = 0.7*etaTelemetry + 0.3*etaScheduled
		etaMethod = "hybrid"
	} else if avgSpeed > 0 {
		etaHours = etaTelemetry
		etaMethod = "telemetry"
	} else if etaScheduled > 0 {
		etaHours = etaScheduled
		etaMethod = "scheduled"
	} else {
		etaHours = etaTelemetry
		etaMethod = distMethod
	}

	// Step 7: Arrival + window (±15 min)
	now := time.Now().UTC()
	arrivalAt := now.Add(time.Duration(etaHours * float64(time.Hour)))

	// Step 8: Monotonic guard
	arrivalAt = s.applyMonotonicGuard(ctx, tripID, arrivalAt)

	etaMin := arrivalAt.Add(-15 * time.Minute)
	etaMax := arrivalAt.Add(15 * time.Minute)

	// Step 9: Confidence
	confidence := "medium"
	if sampleCount >= 5 && age < 5*time.Minute {
		confidence = "high"
	} else if sampleCount < 5 || age > 10*time.Minute {
		confidence = "low"
	}

	return EtaResult{
		ArrivalAt:   arrivalAt,
		EtaMin:      etaMin,
		EtaMax:      etaMax,
		Method:      etaMethod,
		Confidence:  confidence,
		RemainingKM: remainingKM,
		AvgSpeed:    avgSpeed,
	}, nil
}

// roadFactor converts great-circle distance to an estimated road distance.
const roadFactor = 1.25

func haversineKm(lat1, lng1, lat2, lng2 float64) float64 {
	const r = 6371.0
	dLat := (lat2 - lat1) * math.Pi / 180
	dLng := (lng2 - lng1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*
			math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * r * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}
