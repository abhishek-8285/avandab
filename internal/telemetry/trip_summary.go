package telemetry

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"transport-app/internal/shared"
)

// TripSummary is the trip-context payload for the tracking detail sheet
// (route names, schedule, execution timeline, cargo, driver). Read-only
// projection over trips/routes/bookings/drivers/vehicles — no aggregate
// needed. Mounted inside RequireAPIAuth, tenant scoped via trips.tenant_id.
type TripSummary struct {
	TripID        string     `json:"trip_id"`
	TripNumber    string     `json:"trip_number,omitempty"`
	Status        string     `json:"status,omitempty"`
	Origin        string     `json:"origin,omitempty"`
	Destination   string     `json:"destination,omitempty"`
	RouteKM       *float64   `json:"route_km,omitempty"`
	EstHours      *float64   `json:"estimated_hours,omitempty"`
	DepartureTime *time.Time `json:"departure_time,omitempty"`
	ArrivalTime   *time.Time `json:"arrival_time,omitempty"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	ReachedPickup *time.Time `json:"reached_pickup_at,omitempty"`
	InTransitAt   *time.Time `json:"in_transit_at,omitempty"`
	DeliveredAt   *time.Time `json:"delivered_at,omitempty"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
	CargoWeightKG *float64   `json:"cargo_weight_kg,omitempty"`
	VehicleNumber string     `json:"vehicle_number,omitempty"`
	DriverName    string     `json:"driver_name,omitempty"`
	DriverPhone   string     `json:"driver_phone,omitempty"`
}

// TripSummaryHandler serves GET /api/v1/trips/{id}/summary — trip context
// for the live-tracking detail sheet (mirrors the /live feed's read model).
// 404 for unknown ids or trips outside the caller's tenant.
func TripSummaryHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tripID := chi.URLParam(r, "id")
		if tripID == "" {
			tripID = r.URL.Query().Get("trip_id")
		}
		if tripID == "" {
			http.Error(w, `{"error":"trip id required"}`, http.StatusBadRequest)
			return
		}
		tenantID := string(shared.TenantIDFromContext(r.Context()))
		if tenantID == "" {
			http.Error(w, `{"error":"authentication required"}`, http.StatusUnauthorized)
			return
		}

		const q = `
			SELECT t.trip_number, t.status, t.departure_time, t.arrival_time,
			       t.started_at, t.reached_pickup_at, t.in_transit_at, t.delivered_at, t.completed_at,
			       rt.source, rt.destination, rt.distance, rt.estimated_hours,
			       b.cargo_weight,
			       COALESCE(v.vehicle_number, v.registration_number, '') as vehicle_num,
			       COALESCE(NULLIF(TRIM(COALESCE(d.first_name, '') || ' ' || COALESCE(d.last_name, '')), ''), '') as driver_name,
			       COALESCE(d.phone, '') as driver_phone
			FROM trips t
			JOIN routes rt ON rt.id = t.route_id
			LEFT JOIN bookings b ON b.id = t.booking_id
			LEFT JOIN vehicles v ON v.id = t.vehicle_id
			LEFT JOIN drivers d ON d.id = t.driver_id
			WHERE t.id = ? AND t.tenant_id = ?`
		var s TripSummary
		var tripNumber, status, origin, dest, vehNum, driverName, driverPhone sql.NullString
		var departure sql.NullTime
		var arrival, started, reachedPickup, inTransit, delivered, completed sql.NullTime
		var routeKM, estHours, cargo sql.NullFloat64
		err := db.QueryRowContext(r.Context(), q, tripID, tenantID).Scan(
			&tripNumber, &status, &departure, &arrival,
			&started, &reachedPickup, &inTransit, &delivered, &completed,
			&origin, &dest, &routeKM, &estHours,
			&cargo, &vehNum, &driverName, &driverPhone)
		if err == sql.ErrNoRows {
			http.Error(w, `{"error":"trip not found"}`, http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, `{"error":"summary query failed"}`, http.StatusInternalServerError)
			return
		}

		s.TripID = tripID
		s.TripNumber = tripNumber.String
		s.Status = status.String
		s.Origin = origin.String
		s.Destination = dest.String
		s.VehicleNumber = vehNum.String
		s.DriverName = driverName.String
		s.DriverPhone = driverPhone.String
		if departure.Valid {
			t := departure.Time.UTC()
			s.DepartureTime = &t
		}
		if arrival.Valid {
			t := arrival.Time.UTC()
			s.ArrivalTime = &t
		}
		if started.Valid {
			t := started.Time.UTC()
			s.StartedAt = &t
		}
		if reachedPickup.Valid {
			t := reachedPickup.Time.UTC()
			s.ReachedPickup = &t
		}
		if inTransit.Valid {
			t := inTransit.Time.UTC()
			s.InTransitAt = &t
		}
		if delivered.Valid {
			t := delivered.Time.UTC()
			s.DeliveredAt = &t
		}
		if completed.Valid {
			t := completed.Time.UTC()
			s.CompletedAt = &t
		}
		if routeKM.Valid && routeKM.Float64 > 0 {
			v := routeKM.Float64
			s.RouteKM = &v
		}
		if estHours.Valid && estHours.Float64 > 0 {
			v := estHours.Float64
			s.EstHours = &v
		}
		if cargo.Valid && cargo.Float64 > 0 {
			v := cargo.Float64
			s.CargoWeightKG = &v
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(s)
	}
}
