package telemetry

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"transport-app/internal/shared"
)

// PlaybackSummary is the server-side computed summary for a trip playback.
// All distances are kilometres, speeds km/h, durations seconds.
type PlaybackSummary struct {
	TotalPoints     int     `json:"total_points"`
	ReturnedPoints  int     `json:"returned_points"`
	DistanceKM      float64 `json:"distance_km"`
	DurationSeconds int64   `json:"duration_seconds"`
	AvgSpeedKMH     float64 `json:"avg_speed_kmh"`
	MaxSpeedKMH     float64 `json:"max_speed_kmh"`
}

// StopDTO is one detected stop (>5 min, <3 km/h, <200 m drift).
type StopDTO struct {
	StartTime       time.Time `json:"start_time"`
	EndTime         time.Time `json:"end_time"`
	DurationSeconds int64     `json:"duration_seconds"`
	Lat             float64   `json:"lat"`
	Lng             float64   `json:"lng"`
	PointIndex      int       `json:"point_index"`
}

// GeofenceEventDTO is a geofence transition during the trip window.
type GeofenceEventDTO struct {
	ID         string    `json:"id"`
	GeofenceID string    `json:"geofence_id,omitempty"`
	ZoneKind   string    `json:"zone_kind,omitempty"`
	EventType  string    `json:"event_type"`
	Lat        *float64  `json:"lat,omitempty"`
	Lng        *float64  `json:"lng,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	Details    string    `json:"details,omitempty"`
}

// DetentionDTO is a pickup/drop dwell window for the trip.
type DetentionDTO struct {
	ID           string     `json:"id"`
	ZoneKind     string     `json:"zone_kind"`
	ZoneName     string     `json:"zone_name,omitempty"`
	EnteredAt    time.Time  `json:"entered_at"`
	ExitedAt     *time.Time `json:"exited_at,omitempty"`
	DwellSeconds int64      `json:"dwell_seconds"`
	Amount       float64    `json:"amount"`
	Status       string     `json:"status"`
}

// PlaybackResponse is the enriched trip playback payload.
type PlaybackResponse struct {
	TripID         string             `json:"trip_id"`
	VehicleID      string             `json:"vehicle_id,omitempty"`
	Points         []HistoryPoint     `json:"points"`
	Summary        PlaybackSummary    `json:"summary"`
	Stops          []StopDTO          `json:"stops"`
	GeofenceEvents []GeofenceEventDTO `json:"geofence_events"`
	Detentions     []DetentionDTO     `json:"detentions"`
	Truncated      bool               `json:"truncated"`
	NextFrom       *time.Time         `json:"next_from,omitempty"`
	NextID         string             `json:"next_id,omitempty"`
	NextLimit      int                `json:"next_limit,omitempty"`
}

// PlaybackHandler serves GET /api/v1/trips/{id}/playback and
// GET /api/v1/telemetry/playback?trip_id= — the enriched trip
// playback with server-side distance, stops, geofence overlay and
// detention windows. Tenant scoped. Mounted inside RequireAPIAuth.
func PlaybackHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tripID := chi.URLParam(r, "id")
		if tripID == "" {
			tripID = r.PathValue("id")
		}
		if tripID == "" {
			tripID = r.URL.Query().Get("trip_id")
		}
		if tripID == "" {
			http.Error(w, `{"error":"trip_id required"}`, http.StatusBadRequest)
			return
		}
		vehicleID := r.URL.Query().Get("vehicle_id")
		limit := 2000
		if l := r.URL.Query().Get("limit"); l != "" {
			if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
				if parsed > 5000 {
					parsed = 5000
				}
				limit = parsed
			}
		}
		var since *time.Time
		var until *time.Time
		var afterTime *time.Time
		var afterID string
		parseTime := func(v string) *time.Time {
			if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
				return &t
			}
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				return &t
			}
			if t, err := time.Parse("2006-01-02T15:04:05", v); err == nil {
				return &t
			}
			return nil
		}
		if v := r.URL.Query().Get("from"); v != "" {
			since = parseTime(v)
		}
		if v := r.URL.Query().Get("after"); v != "" {
			afterTime = parseTime(v)
		}
		afterID = r.URL.Query().Get("after_id")
		if v := r.URL.Query().Get("to"); v != "" {
			until = parseTime(v)
		}
		tenantID := string(shared.TenantIDFromContext(r.Context()))
		if tenantID == "" {
			http.Error(w, `{"error":"authentication required"}`, http.StatusUnauthorized)
			return
		}
		points, truncated, nextFrom, nextID, err := fetchHistoryPointsWithCursor(r.Context(), db, tenantID, vehicleID, tripID, since, until, afterTime, afterID, limit)
		if err != nil {
			http.Error(w, `{"error":"playback query failed"}`, http.StatusInternalServerError)
			return
		}
		// Resolve vehicle_id from first point when caller didn't filter by it
		resolvedVehicleID := vehicleID
		if resolvedVehicleID == "" && len(points) > 0 {
			for _, p := range points {
				if p.VehicleID != "" {
					resolvedVehicleID = p.VehicleID
					break
				}
			}
			// fallback: look up trip's current vehicle_id
			if resolvedVehicleID == "" {
				var vid sql.NullString
				_ = db.QueryRowContext(r.Context(), `SELECT vehicle_id FROM trips WHERE id = ? AND tenant_id = ?`, tripID, tenantID).Scan(&vid)
				if vid.Valid {
					resolvedVehicleID = vid.String
				}
			}
		}

		summary, stops := computeSummary(points)
		geofences, _ := fetchGeofenceEvents(r.Context(), db, tenantID, tripID, since, until)
		detentions, _ := fetchDetentions(r.Context(), db, tenantID, tripID)

		resp := PlaybackResponse{
			TripID:         tripID,
			VehicleID:      resolvedVehicleID,
			Points:         points,
			Summary:        summary,
			Stops:          stops,
			GeofenceEvents: geofences,
			Detentions:     detentions,
			Truncated:      truncated,
			NextFrom:       nextFrom,
			NextID:         nextID,
		}
		if truncated && nextFrom != nil {
			resp.NextLimit = limit
			w.Header().Set("X-Truncated", "true")
			w.Header().Set("X-Next-From", nextFrom.UTC().Format(time.RFC3339Nano))
			if nextID != "" {
				w.Header().Set("X-Next-ID", nextID)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func isStopped(p HistoryPoint) bool {
	if p.Ignition != nil {
		if !*p.Ignition {
			return true
		}
	}
	return p.Speed < 3.0
}

func computeSummary(points []HistoryPoint) (PlaybackSummary, []StopDTO) {
	if len(points) == 0 {
		return PlaybackSummary{}, nil
	}
	// Prefer odometer delta when the trip has continuous odometer coverage;
	// otherwise fall back to haversine sum. Odometer is cumulative km, so
	// delta = last - first when both ends are valid and positive.
	var dist float64
	useOdo := false
	if points[0].Odometer != nil && points[len(points)-1].Odometer != nil {
		d := *points[len(points)-1].Odometer - *points[0].Odometer
		if d > 0 && d < 10000 {
			// Cross-check against haversine to guard against odometer jumps
			// (e.g., device replacement). Accept odometer if within 2x haversine.
			var hav float64
			for i := 1; i < len(points); i++ {
				hav += serverHaversineKM(points[i-1], points[i])
			}
			if hav == 0 || (d >= hav*0.3 && d <= hav*3.0) {
				dist = d
				useOdo = true
			}
		}
	}
	if !useOdo {
		for i := 1; i < len(points); i++ {
			dist += serverHaversineKM(points[i-1], points[i])
		}
	}
	var maxSp float64
	for _, p := range points {
		if p.Speed > maxSp {
			maxSp = p.Speed
		}
	}
	t0 := points[0].Ts
	t1 := points[len(points)-1].Ts
	durSec := int64(math.Max(0, t1.Sub(t0).Seconds()))
	var avg float64
	if durSec > 0 {
		avg = dist / (float64(durSec) / 3600.0)
	}
	stops := detectStops(points)
	summary := PlaybackSummary{
		TotalPoints:     len(points),
		ReturnedPoints:  len(points),
		DistanceKM:      math.Round(dist*10) / 10,
		DurationSeconds: durSec,
		AvgSpeedKMH:     math.Round(avg*10) / 10,
		MaxSpeedKMH:     math.Round(maxSp*10) / 10,
	}
	return summary, stops
}

// detectStops finds dwell windows: ignition-off OR speed <3 km/h sustained
// >=5 min and max drift <200 m (excludes slow crawls). Requires >=2 points.
func detectStops(points []HistoryPoint) []StopDTO {
	const (
		dwellMin   = 5 * 60 // seconds
		driftMaxKM = 0.20   // km (200 m)
	)
	var stops []StopDTO
	n := len(points)
	i := 0
	for i < n {
		if !isStopped(points[i]) {
			i++
			continue
		}
		// start of a low-speed / ignition-off run
		start := i
		end := i
		for end < n && isStopped(points[end]) {
			end++
		}
		// [start, end) is the run
		if end-start < 2 {
			i = end
			continue
		}
		dur := points[end-1].Ts.Sub(points[start].Ts).Seconds()
		if dur < dwellMin {
			i = end
			continue
		}
		// drift check: max distance from start point to any point in run
		maxDrift := 0.0
		for k := start; k < end; k++ {
			d := serverHaversineKM(points[start], points[k])
			if d > maxDrift {
				maxDrift = d
			}
		}
		if maxDrift > driftMaxKM {
			i = end
			continue
		}
		mid := points[(start+end-1)/2]
		stops = append(stops, StopDTO{
			StartTime:       points[start].Ts,
			EndTime:         points[end-1].Ts,
			DurationSeconds: int64(dur),
			Lat:             mid.Lat,
			Lng:             mid.Lng,
			PointIndex:      start,
		})
		i = end
	}
	return stops
}

func fetchGeofenceEvents(ctx context.Context, db *sql.DB, tenantID, tripID string, since, until *time.Time) ([]GeofenceEventDTO, error) {
	query := `SELECT id, geofence_id, zone_kind, event_type, latitude, longitude, created_at, details
	          FROM geofence_events
	          WHERE tenant_id = ? AND trip_id = ?`
	args := []any{tenantID, tripID}
	if since != nil {
		query += ` AND created_at >= ?`
		args = append(args, since.UTC().Format("2006-01-02 15:04:05"))
	}
	if until != nil {
		query += ` AND created_at <= ?`
		args = append(args, until.UTC().Format("2006-01-02 15:04:05"))
	}
	query += ` ORDER BY created_at ASC LIMIT 200`
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []GeofenceEventDTO
	for rows.Next() {
		var e GeofenceEventDTO
		var gid, zone sql.NullString
		var lat, lng sql.NullFloat64
		var details sql.NullString
		var created time.Time
		if err := rows.Scan(&e.ID, &gid, &zone, &e.EventType, &lat, &lng, &created, &details); err != nil {
			return nil, err
		}
		if gid.Valid {
			e.GeofenceID = gid.String
		}
		if zone.Valid {
			e.ZoneKind = zone.String
		}
		if lat.Valid {
			v := lat.Float64
			e.Lat = &v
		}
		if lng.Valid {
			v := lng.Float64
			e.Lng = &v
		}
		e.CreatedAt = created.UTC()
		if details.Valid {
			e.Details = details.String
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func fetchDetentions(ctx context.Context, db *sql.DB, tenantID, tripID string) ([]DetentionDTO, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT d.id, d.zone_kind, COALESCE(g.name,''), d.entered_at, d.exited_at, d.dwell_seconds, d.amount, d.status
		FROM trip_detentions d
		LEFT JOIN geofences g ON g.id = d.geofence_id
		WHERE d.tenant_id = ? AND d.trip_id = ?
		ORDER BY d.entered_at ASC`, tenantID, tripID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []DetentionDTO
	for rows.Next() {
		var d DetentionDTO
		var exited sql.NullTime
		var amount sql.NullFloat64
		if err := rows.Scan(&d.ID, &d.ZoneKind, &d.ZoneName, &d.EnteredAt, &exited, &d.DwellSeconds, &amount, &d.Status); err != nil {
			return nil, err
		}
		if exited.Valid {
			t := exited.Time.UTC()
			d.ExitedAt = &t
		}
		if amount.Valid {
			d.Amount = amount.Float64
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
