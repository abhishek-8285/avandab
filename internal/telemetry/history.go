package telemetry

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"time"

	"transport-app/internal/shared"
)

// HistoryPoint is one breadcrumb on a vehicle's recent trail.
// Extended for trip playback: heading/odometer/trip/vehicle are optional so
// old clients keep working while the playback UI animates with direction.
// ID and Ignition are exposed for keyset pagination and ignition-aware stops.
type HistoryPoint struct {
	ID        string    `json:"id,omitempty"`
	Lat       float64   `json:"lat"`
	Lng       float64   `json:"lng"`
	Speed     float64   `json:"speed"`
	Ts        time.Time `json:"ts"`
	Heading   *float64  `json:"heading,omitempty"`
	Odometer  *float64  `json:"odometer,omitempty"`
	TripID    string    `json:"trip_id,omitempty"`
	VehicleID string    `json:"vehicle_id,omitempty"`
	Ignition  *bool     `json:"ignition,omitempty"`
}

// HistoryHandler serves GET /api/v1/telemetry/history — the breadcrumb trail.
// Modes:
//   - vehicle trail: ?vehicle_id= (required) & ?minutes=90 (cap 24h) — legacy
//   - trip playback: ?trip_id= (requires trip ownership) with optional
//     ?vehicle_id= & ?from=&to= (RFC3339) & ?limit= (default 500, cap 2000)
//
// Tenant scoped via vehicles/trips joins (telemetry_snapshots has no tenant
// column). Mounted inside RequireAPIAuth.
func HistoryHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vehicleID := r.URL.Query().Get("vehicle_id")
		tripID := r.URL.Query().Get("trip_id")
		fromStr := r.URL.Query().Get("from")
		toStr := r.URL.Query().Get("to")
		if vehicleID == "" && tripID == "" {
			http.Error(w, `{"error":"vehicle_id or trip_id required"}`, http.StatusBadRequest)
			return
		}
		limit := 500
		if l := r.URL.Query().Get("limit"); l != "" {
			if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
				if parsed > 2000 {
					parsed = 2000
				}
				limit = parsed
			}
		}
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
		var since *time.Time
		var until *time.Time
		var afterTime *time.Time
		var afterID string
		if fromStr != "" {
			since = parseTime(fromStr)
		}
		if toStr != "" {
			until = parseTime(toStr)
		}
		if v := r.URL.Query().Get("after"); v != "" {
			afterTime = parseTime(v)
		}
		afterID = r.URL.Query().Get("after_id")
		if since == nil && tripID == "" {
			minutes := 90
			if m := r.URL.Query().Get("minutes"); m != "" {
				if parsed, err := strconv.Atoi(m); err == nil && parsed > 0 && parsed <= 7*24*60 {
					minutes = parsed
				}
			}
			t := time.Now().UTC().Add(-time.Duration(minutes) * time.Minute)
			since = &t
		}
		tenantID := string(shared.TenantIDFromContext(r.Context()))
		if tenantID == "" {
			http.Error(w, `{"error":"authentication required"}`, http.StatusUnauthorized)
			return
		}
		// Dual-source: prefer telemetry_positions (per-frame history,
		// tenant_id direct, indexed on vehicle_id/device_time) and fall back
		// to telemetry_snapshots for older data (pre-00041). Both share the
		// same HistoryPoint shape; positions carries heading/odometer natively.
		// Keyset pagination via afterTime/afterID handles same-second duplicates.
		points, truncated, nextFrom, nextID, err := fetchHistoryPointsWithCursor(r.Context(), db, tenantID, vehicleID, tripID, since, until, afterTime, afterID, limit)
		if err != nil {
			http.Error(w, `{"error":"history query failed"}`, http.StatusInternalServerError)
			return
		}
		if truncated && nextFrom != nil {
			w.Header().Set("X-Truncated", "true")
			w.Header().Set("X-Next-From", nextFrom.UTC().Format(time.RFC3339Nano))
			if nextID != "" {
				w.Header().Set("X-Next-ID", nextID)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(points)
	}
}

// fetchHistoryPoints is the dual-source wrapper kept for tests.
// Prefer fetchHistoryPointsWithCursor for new code.

//nolint:unused
func fetchHistoryPoints(ctx context.Context, db *sql.DB, tenantID, vehicleID, tripID string, since, until *time.Time, limit int) ([]HistoryPoint, bool, *time.Time, error) {
	pts, trunc, nxt, _, err := fetchHistoryPointsWithCursor(ctx, db, tenantID, vehicleID, tripID, since, until, nil, "", limit)
	return pts, trunc, nxt, err
}

func fetchHistoryPointsWithCursor(ctx context.Context, db *sql.DB, tenantID, vehicleID, tripID string, since, until, after *time.Time, afterID string, limit int) ([]HistoryPoint, bool, *time.Time, string, error) {
	probeLimit := limit + 1
	pts, err := queryPositionsWithCursor(ctx, db, tenantID, vehicleID, tripID, since, until, after, afterID, probeLimit)
	if err != nil {
		return nil, false, nil, "", err
	}
	if len(pts) > 0 {
		return truncatePointsWithCursor(pts, limit)
	}
	pts, err = querySnapshotsWithCursor(ctx, db, tenantID, vehicleID, tripID, since, until, after, afterID, probeLimit)
	if err != nil {
		return nil, false, nil, "", err
	}
	return truncatePointsWithCursor(pts, limit)
}

func truncatePointsWithCursor(pts []HistoryPoint, limit int) ([]HistoryPoint, bool, *time.Time, string, error) {
	if len(pts) <= limit {
		return pts, false, nil, "", nil
	}
	truncated := pts[:limit]
	next := truncated[len(truncated)-1].Ts
	nextID := truncated[len(truncated)-1].ID
	return truncated, true, &next, nextID, nil
}

func queryPositionsWithCursor(ctx context.Context, db *sql.DB, tenantID, vehicleID, tripID string, since, until, after *time.Time, afterID string, limit int) ([]HistoryPoint, error) {
	query := `SELECT id, latitude, longitude, COALESCE(speed,0), device_time, heading, odometer, trip_id, vehicle_id, ignition
	          FROM telemetry_positions
	          WHERE tenant_id = ? AND latitude IS NOT NULL AND longitude IS NOT NULL`
	args := []any{tenantID}
	if vehicleID != "" {
		query += ` AND vehicle_id = ?`
		args = append(args, vehicleID)
	}
	if tripID != "" {
		query += ` AND trip_id = ?`
		args = append(args, tripID)
	}
	if since != nil {
		query += ` AND device_time >= ?`
		args = append(args, since.UTC().Format("2006-01-02 15:04:05"))
	}
	if until != nil {
		query += ` AND device_time <= ?`
		args = append(args, until.UTC().Format("2006-01-02 15:04:05"))
	}
	if after != nil {
		if afterID != "" {
			query += ` AND (device_time > ? OR (device_time = ? AND id > ?))`
			args = append(args, after.UTC().Format("2006-01-02 15:04:05"), after.UTC().Format("2006-01-02 15:04:05"), afterID)
		} else {
			query += ` AND device_time > ?`
			args = append(args, after.UTC().Format("2006-01-02 15:04:05"))
		}
	}
	query += ` ORDER BY device_time ASC, id ASC LIMIT ?`
	args = append(args, limit)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanPoints(rows)
}

func querySnapshotsWithCursor(ctx context.Context, db *sql.DB, tenantID, vehicleID, tripID string, since, until, after *time.Time, afterID string, limit int) ([]HistoryPoint, error) {
	hasHeading := columnExists(db, "telemetry_snapshots", "heading")
	headingSel := "NULL"
	if hasHeading {
		headingSel = "s.heading"
	}
	hasIgnition := columnExists(db, "telemetry_snapshots", "ignition")
	ignitionSel := "NULL"
	if hasIgnition {
		ignitionSel = "s.ignition"
	}
	var query string
	var args []any
	if tripID != "" {
		query = `SELECT s.id, s.latitude, s.longitude, COALESCE(s.speed,0), s.timestamp, ` + headingSel + `, s.odometer, s.trip_id, s.vehicle_id, ` + ignitionSel + `
		         FROM telemetry_snapshots s
		         JOIN trips t ON t.id = s.trip_id AND t.tenant_id = ?
		         WHERE s.latitude IS NOT NULL AND s.longitude IS NOT NULL AND s.trip_id = ?`
		args = []any{tenantID, tripID}
		if vehicleID != "" {
			query += ` AND s.vehicle_id = ?`
			args = append(args, vehicleID)
		}
	} else {
		query = `SELECT s.id, s.latitude, s.longitude, COALESCE(s.speed,0), s.timestamp, ` + headingSel + `, s.odometer, s.trip_id, s.vehicle_id, ` + ignitionSel + `
		         FROM telemetry_snapshots s
		         JOIN vehicles v ON v.id = s.vehicle_id AND v.tenant_id = ?
		         WHERE s.latitude IS NOT NULL AND s.longitude IS NOT NULL AND s.vehicle_id = ?`
		args = []any{tenantID, vehicleID}
	}
	if since != nil {
		query += ` AND s.timestamp >= ?`
		args = append(args, since.UTC().Format("2006-01-02 15:04:05"))
	}
	if until != nil {
		query += ` AND s.timestamp <= ?`
		args = append(args, until.UTC().Format("2006-01-02 15:04:05"))
	}
	if after != nil {
		if afterID != "" {
			query += ` AND (s.timestamp > ? OR (s.timestamp = ? AND s.id > ?))`
			args = append(args, after.UTC().Format("2006-01-02 15:04:05"), after.UTC().Format("2006-01-02 15:04:05"), afterID)
		} else {
			query += ` AND s.timestamp > ?`
			args = append(args, after.UTC().Format("2006-01-02 15:04:05"))
		}
	}
	query += ` ORDER BY s.timestamp ASC, s.id ASC LIMIT ?`
	args = append(args, limit)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanPoints(rows)
}

func scanPoints(rows *sql.Rows) ([]HistoryPoint, error) {
	var out []HistoryPoint
	for rows.Next() {
		var p HistoryPoint
		var ts time.Time
		var id sql.NullString
		var heading sql.NullFloat64
		var odometer sql.NullFloat64
		var tripVal, vehVal sql.NullString
		var ignition sql.NullInt64
		if err := rows.Scan(&id, &p.Lat, &p.Lng, &p.Speed, &ts, &heading, &odometer, &tripVal, &vehVal, &ignition); err != nil {
			return nil, err
		}
		p.Ts = ts.UTC()
		if id.Valid {
			p.ID = id.String
		}
		if heading.Valid {
			h := heading.Float64
			p.Heading = &h
		}
		if odometer.Valid {
			o := odometer.Float64
			p.Odometer = &o
		}
		if tripVal.Valid {
			p.TripID = tripVal.String
		}
		if vehVal.Valid {
			p.VehicleID = vehVal.String
		}
		if ignition.Valid {
			b := ignition.Int64 != 0
			p.Ignition = &b
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// serverHaversineKM computes great-circle distance in kilometres.
func serverHaversineKM(a, b HistoryPoint) float64 {
	const R = 6371.0088
	toRad := func(d float64) float64 { return d * math.Pi / 180 }
	dLat := toRad(b.Lat - a.Lat)
	dLng := toRad(b.Lng - a.Lng)
	sa := math.Sin(dLat / 2)
	so := math.Sin(dLng / 2)
	h := sa*sa + math.Cos(toRad(a.Lat))*math.Cos(toRad(b.Lat))*so*so
	return 2 * R * math.Asin(math.Sqrt(h))
}
