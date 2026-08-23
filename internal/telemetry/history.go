package telemetry

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"transport-app/internal/shared"
)

// HistoryPoint is one breadcrumb on a vehicle's recent trail.
type HistoryPoint struct {
	Lat   float64   `json:"lat"`
	Lng   float64   `json:"lng"`
	Speed float64   `json:"speed"`
	Ts    time.Time `json:"ts"`
}

// HistoryHandler serves GET /api/v1/telemetry/history — the breadcrumb trail
// for ONE vehicle (?vehicle_id= required; ?minutes= window, default 90,
// capped at 24h). Returns up to 500 points ascending by time so clients can
// draw a polyline directly. Tenant scoped via the vehicles join
// (telemetry_snapshots has no tenant column). Mounted inside RequireAPIAuth.
func HistoryHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vehicleID := r.URL.Query().Get("vehicle_id")
		if vehicleID == "" {
			http.Error(w, `{"error":"vehicle_id required"}`, http.StatusBadRequest)
			return
		}
		minutes := 90
		if m := r.URL.Query().Get("minutes"); m != "" {
			if parsed, err := strconv.Atoi(m); err == nil && parsed > 0 && parsed <= 24*60 {
				minutes = parsed
			}
		}
		tenantID := string(shared.TenantIDFromContext(r.Context()))
		if tenantID == "" {
			tenantID = string(shared.DefaultTenant)
		}
		since := time.Now().UTC().Add(-time.Duration(minutes) * time.Minute)
		rows, err := db.QueryContext(r.Context(), `
			SELECT s.latitude, s.longitude, COALESCE(s.speed, 0), s.timestamp
			FROM telemetry_snapshots s
			JOIN vehicles v ON v.id = s.vehicle_id AND v.tenant_id = ?
			WHERE s.vehicle_id = ? AND s.latitude IS NOT NULL AND s.longitude IS NOT NULL
			  AND s.timestamp >= ?
			ORDER BY s.timestamp ASC
			LIMIT 500`,
			tenantID, vehicleID, since.Format("2006-01-02 15:04:05"))
		if err != nil {
			http.Error(w, `{"error":"history query failed"}`, http.StatusInternalServerError)
			return
		}
		defer func() { _ = rows.Close() }()
		out := []HistoryPoint{}
		for rows.Next() {
			var p HistoryPoint
			var ts time.Time
			if err := rows.Scan(&p.Lat, &p.Lng, &p.Speed, &ts); err != nil {
				http.Error(w, `{"error":"history scan failed"}`, http.StatusInternalServerError)
				return
			}
			p.Ts = ts.UTC()
			out = append(out, p)
		}
		if err := rows.Err(); err != nil {
			http.Error(w, `{"error":"history rows failed"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(out)
	}
}
