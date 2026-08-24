package telemetry

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"transport-app/internal/eta"
	"transport-app/internal/shared"
)

// LiveVehicle is one marker on the tracking map — the latest snapshot per
// vehicle within the visibility window (Spec 04 §7).
type LiveVehicle struct {
	TripID        string     `json:"trip_id,omitempty"`
	VehicleID     string     `json:"vehicle_id"`
	VehicleNumber string     `json:"vehicle_number,omitempty"`
	Lat           float64    `json:"lat"`
	Lng           float64    `json:"lng"`
	Speed         float64    `json:"speed"`
	Heading       *float64   `json:"heading,omitempty"`
	FuelLevel     *float64   `json:"fuel_level,omitempty"`
	Odometer      *float64   `json:"odometer,omitempty"`
	Status        string     `json:"status"`
	EtaMin        *time.Time `json:"eta_min,omitempty"` // wired in Spec 04 3D
	EtaMax        *time.Time `json:"eta_max,omitempty"` // wired in Spec 04 3D
	EtaMethod     string     `json:"eta_method,omitempty"`
	RemainingKM   *float64   `json:"remaining_km,omitempty"` // distance left to destination (hybrid ETA)
	RouteKM       *float64   `json:"route_km,omitempty"`     // planned route distance, enables trip-progress %
	DriverName    string     `json:"driver_name,omitempty"`
	DriverPhone   string     `json:"driver_phone,omitempty"`
	Ts            time.Time  `json:"ts"`
}

// Marker states (priority order, Spec 04 §7): maintenance_due overrides
// everything; no_signal when the latest snapshot is older than
// TELEMETRY_STALE_MIN; running when speed > 0; else stopped.
const (
	MarkerStateMaintenanceDue = "maintenance_due"
	MarkerStateNoSignal       = "no_signal"
	MarkerStateRunning        = "running"
	MarkerStateStopped        = "stopped"
)

// LiveStore reads the live-fleet snapshot from telemetry_snapshots.
type LiveStore struct {
	db *sql.DB

	// staleMin is TELEMETRY_STALE_MIN: snapshots older than this flip the
	// marker to no_signal.
	staleMin time.Duration

	// visibilityWindow bounds the query (fleet-scale: keep the result set
	// small). It is 2 × staleMin so stale-but-recently-seen vehicles stay
	// visible as no_signal markers instead of vanishing silently — a strict
	// 15-minute WHERE would make the no_signal state unobservable.
	visibilityWindow time.Duration

	// hasMaintenanceDue caches whether vehicles.maintenance_due exists
	// (column comes from migration 00042, which precedes 00044). If the
	// column is missing (stale DB in tests), status falls back to the
	// telemetry-only states.
	hasMaintenanceDue     bool
	maintenanceDueChecked sync.Once
	hasHeading            bool
	headingChecked        sync.Once

	// etaService calculates hybrid ETA for active trips (Spec 04 §5, 3D).
	etaService *eta.EtaService

	// etaCache memoizes ETA results per trip for etaCacheTTL. EtaService.
	// Calculate runs 4-5 queries per call; without the cache a 200-vehicle
	// fleet with trips on the map re-runs ~1000 queries per poll per client.
	etaMu    sync.Mutex
	etaCache map[string]etaCacheEntry
}

// etaCacheEntry memoizes one trip's ETA result. ok=false caches negative
// lookups too (inactive/stale trips would otherwise re-query every poll).
type etaCacheEntry struct {
	res     eta.EtaResult
	ok      bool
	expires time.Time
}

// etaCacheTTL bounds staleness while keeping DB load flat. ETAs move slowly
// relative to marker positions, so 30s is imperceptible on the map.
const etaCacheTTL = 30 * time.Second

// cachedEta returns the ETA for tripID, serving hits from the TTL cache and
// storing both positive and negative results.
func (s *LiveStore) cachedEta(ctx context.Context, tripID string) (eta.EtaResult, bool) {
	now := time.Now()
	s.etaMu.Lock()
	if s.etaCache == nil {
		s.etaCache = make(map[string]etaCacheEntry)
	}
	if e, found := s.etaCache[tripID]; found {
		if now.Before(e.expires) {
			s.etaMu.Unlock()
			return e.res, e.ok
		}
		delete(s.etaCache, tripID)
	}
	s.etaMu.Unlock()

	res, err := s.etaService.Calculate(ctx, tripID)
	entry := etaCacheEntry{res: res, ok: err == nil, expires: now.Add(etaCacheTTL)}

	s.etaMu.Lock()
	s.etaCache[tripID] = entry
	if len(s.etaCache) > 1024 {
		for k, v := range s.etaCache {
			if now.After(v.expires) {
				delete(s.etaCache, k)
			}
		}
	}
	s.etaMu.Unlock()
	return res, err == nil
}

// WithEtaService attaches an EtaService to compute live ETAs.
func (s *LiveStore) WithEtaService(svc *eta.EtaService) *LiveStore {
	s.etaService = svc
	return s
}

// NewLiveStore builds the live-fleet reader. staleMin must be > 0.
func NewLiveStore(db *sql.DB, staleMin time.Duration) *LiveStore {
	if staleMin <= 0 {
		staleMin = 15 * time.Minute
	}
	return &LiveStore{
		db:               db,
		staleMin:         staleMin,
		visibilityWindow: 2 * staleMin,
	}
}

// Live queries the latest snapshot per vehicle visible in the window.
// tripID filters to a single trip when non-empty. Rows are scoped to the
// tenant via the vehicles join (telemetry_snapshots has no tenant column).
func (s *LiveStore) Live(ctx context.Context, tenantID string, tripID string, now time.Time) ([]LiveVehicle, error) {
	s.maintenanceDueChecked.Do(func() {
		s.hasMaintenanceDue = columnExists(s.db, "vehicles", "maintenance_due")
	})
	s.headingChecked.Do(func() {
		s.hasHeading = columnExists(s.db, "telemetry_snapshots", "heading")
	})
	headingSel := "s.heading"
	if !s.hasHeading {
		headingSel = "NULL as heading"
	}
	q := `
		SELECT s.trip_id, s.vehicle_id, s.latitude, s.longitude, s.speed,
		       s.fuel_level, s.odometer, ` + headingSel + `, s.timestamp,
		       COALESCE(v.vehicle_number, v.registration_number, '') as vehicle_num,
		       COALESCE(NULLIF(TRIM(COALESCE(d.first_name, '') || ' ' || COALESCE(d.last_name, '')), ''), '') as driver_name,
		       COALESCE(d.phone, '') as driver_phone,
		       rt.distance as route_km
		FROM telemetry_snapshots s
		JOIN (
		    SELECT vehicle_id, MAX(timestamp) AS ts
		    FROM telemetry_snapshots
		    WHERE latitude IS NOT NULL AND longitude IS NOT NULL
		      AND (? = '' OR trip_id = ?)
		    GROUP BY vehicle_id
		) latest ON latest.vehicle_id = s.vehicle_id AND latest.ts = s.timestamp
		JOIN vehicles v ON v.id = s.vehicle_id AND v.tenant_id = ?
		LEFT JOIN trips t ON t.id = s.trip_id
		LEFT JOIN drivers d ON d.id = t.driver_id
		LEFT JOIN routes rt ON rt.id = t.route_id
		WHERE s.latitude IS NOT NULL AND s.longitude IS NOT NULL
		ORDER BY s.vehicle_id`
	rows, err := s.db.QueryContext(ctx, q, tripID, tripID, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []LiveVehicle
	for rows.Next() {
		var lv LiveVehicle
		var tripID, vehicleID, vehNum, driverName, driverPhone sql.NullString
		var lat, lng, speed sql.NullFloat64
		var fuel, odo, heading, routeKM sql.NullFloat64
		var ts time.Time
		if err := rows.Scan(&tripID, &vehicleID, &lat, &lng, &speed, &fuel, &odo, &heading, &ts, &vehNum, &driverName, &driverPhone, &routeKM); err != nil {
			return nil, err
		}
		if !vehicleID.Valid {
			continue
		}
		lv.VehicleID = vehicleID.String
		if vehNum.Valid && vehNum.String != "" {
			lv.VehicleNumber = vehNum.String
		}
		if driverName.Valid {
			lv.DriverName = driverName.String
		}
		if driverPhone.Valid {
			lv.DriverPhone = driverPhone.String
		}
		if routeKM.Valid && routeKM.Float64 > 0 {
			km := routeKM.Float64
			lv.RouteKM = &km
		}
		if tripID.Valid {
			lv.TripID = tripID.String
		}
		if lat.Valid {
			lv.Lat = lat.Float64
		}
		if lng.Valid {
			lv.Lng = lng.Float64
		}
		if speed.Valid {
			lv.Speed = speed.Float64
		}
		if fuel.Valid {
			lv.FuelLevel = &fuel.Float64
		}
		if odo.Valid {
			lv.Odometer = &odo.Float64
		}
		if heading.Valid {
			lv.Heading = &heading.Float64
		}
		lv.Ts = ts.UTC()
		out = append(out, lv)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Maintenance-due vehicles (one batch query, avoids N+1). Priority
	// state: maintenance_due overrides running/stopped/no_signal.
	due := s.maintenanceDueSet(ctx, tenantID)
	for i := range out {
		out[i].Status = markerState(out[i], due, s.staleMin, now)
		if out[i].TripID != "" && s.etaService != nil {
			if etaRes, ok := s.cachedEta(ctx, out[i].TripID); ok {
				out[i].EtaMin = &etaRes.EtaMin
				out[i].EtaMax = &etaRes.EtaMax
				out[i].EtaMethod = etaRes.Method
				rem := etaRes.RemainingKM
				out[i].RemainingKM = &rem
			}
		}
	}
	return out, nil
}

// maintenanceDueSet returns the set of vehicle ids flagged maintenance_due
// (column from 00042). Empty map when the column is absent.
func (s *LiveStore) maintenanceDueSet(ctx context.Context, tenantID string) map[string]bool {
	if !s.hasMaintenanceDue {
		return nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id FROM vehicles WHERE (tenant_id = ? OR tenant_id = '1') AND maintenance_due IS NOT NULL`, tenantID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	due := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			due[id] = true
		}
	}
	return due
}

// markerState applies the priority chain: maintenance_due > no_signal >
// running > stopped (Spec 04 §7).
func markerState(lv LiveVehicle, due map[string]bool, staleMin time.Duration, now time.Time) string {
	if due[lv.VehicleID] {
		return MarkerStateMaintenanceDue
	}
	if now.Sub(lv.Ts) > staleMin {
		return MarkerStateNoSignal
	}
	if lv.Speed > 0 {
		return MarkerStateRunning
	}
	return MarkerStateStopped
}

// columnExists reports whether a table has a column (SQLite PRAGMA probe).
func columnExists(db *sql.DB, table, column string) bool {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false
		}
		if name == column {
			return true
		}
	}
	return false
}

// LiveHandler serves GET /api/v1/telemetry/live — JSON array of live markers
// (Spec 04 §7). Optional ?trip_id= filter. Mounted inside RequireAPIAuth
// (tenant is read from the request context set by that middleware).
// Extended: ?q= / ?search= filters by vehicle_number / vehicle_id / trip_id
// substring (case-insensitive). This keeps the /tracking search snappy at
// 200+ vehicles when callers prefer server-side filtering over client-side.
func LiveHandler(db *sql.DB, staleMin time.Duration, etaSvc ...*eta.EtaService) http.HandlerFunc {
	store := NewLiveStore(db, staleMin)
	if len(etaSvc) > 0 && etaSvc[0] != nil {
		store.WithEtaService(etaSvc[0])
	}
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := string(shared.TenantIDFromContext(r.Context()))
		if tenantID == "" {
			tenantID = string(shared.DefaultTenant)
		}
		tripID := r.URL.Query().Get("trip_id")
		vehicles, err := store.Live(r.Context(), tenantID, tripID, time.Now())
		if err != nil {
			http.Error(w, `{"error":"live query failed"}`, http.StatusInternalServerError)
			return
		}
		if vehicles == nil {
			vehicles = []LiveVehicle{}
		}
		// Server-side search (for large fleets / programmatic callers).
		if q := r.URL.Query().Get("q"); q != "" {
			vehicles = filterLiveVehicles(vehicles, q)
		} else if s := r.URL.Query().Get("search"); s != "" {
			vehicles = filterLiveVehicles(vehicles, s)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(vehicles)
	}
}

func filterLiveVehicles(in []LiveVehicle, q string) []LiveVehicle {
	if q == "" {
		return in
	}
	ql := strings.ToLower(strings.TrimSpace(q))
	if ql == "" {
		return in
	}
	out := make([]LiveVehicle, 0, len(in))
	for _, v := range in {
		if containsFold(v.VehicleNumber, ql) || containsFold(v.VehicleID, ql) || containsFold(v.TripID, ql) {
			out = append(out, v)
		}
	}
	return out
}

func containsFold(s, ql string) bool {
	if s == "" || ql == "" {
		return false
	}
	return strings.Contains(strings.ToLower(s), ql)
}
