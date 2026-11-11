package application

// Agent-facing trip facade, extracted from internal/agent/tools.go
// (handlers-end-state ADR, Phase 3). Listing delegates to the clean
// ListTripsUseCase (tenant scoping/search/status rules); by-number detail uses
// a tenant-scoped join read for the tool JSON.

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"transport-app/internal/shared"
	"transport-app/internal/shared/uow"
)

// AgentTripRow is one list_trips JSON row.
type AgentTripRow struct {
	ID        string `json:"id"`
	Number    string `json:"number"`
	Route     string `json:"route"`
	Driver    string `json:"driver"`
	Vehicle   string `json:"vehicle"`
	Departure string `json:"departure_time"`
	Status    string `json:"status"`
}

// AgentTripDetail is the get_trip JSON shape.
type AgentTripDetail struct {
	ID        string `json:"id"`
	Number    string `json:"number"`
	Route     string `json:"route"`
	Driver    string `json:"driver"`
	Vehicle   string `json:"vehicle"`
	Departure string `json:"departure"`
	Status    string `json:"status"`
	BookingID string `json:"booking_id"`
}

// AgentTripService is the trip facade. Stateless.
type AgentTripService struct{ db *sql.DB }

// NewAgentTripService wires the facade against the database.
func NewAgentTripService(db *sql.DB) *AgentTripService { return &AgentTripService{db: db} }

// List returns the tenant's trips via the owning-slice list use case.
func (s *AgentTripService) List(ctx context.Context, tenantID shared.TenantID, query, status string, limit int) ([]AgentTripRow, error) {
	if s == nil || s.db == nil {
		return nil, sql.ErrNoRows
	}
	uc := NewListTripsUseCase(uow.NewSQLUnitOfWork(s.db))
	res, err := uc.Execute(ctx, ListTripsQuery{TenantID: tenantID, Search: query, Status: status, Limit: limit})
	if err != nil {
		return nil, err
	}
	out := make([]AgentTripRow, 0, len(res.Trips))
	for _, t := range res.Trips {
		drv := strings.TrimSpace(t.DriverFirstName + " " + t.DriverLastName)
		out = append(out, AgentTripRow{
			ID: t.ID, Number: t.TripNumber, Route: t.RouteSource + " -> " + t.RouteDestination,
			Driver: drv, Vehicle: t.VehicleNumber,
			Departure: t.DepartureTime.Format("2006-01-02 15:04"), Status: t.Status,
		})
	}
	return out, nil
}

// GetByNumber returns the tenant's trip detail by trip number.
func (s *AgentTripService) GetByNumber(ctx context.Context, tenantID shared.TenantID, number string) (*AgentTripDetail, error) {
	if s == nil || s.db == nil {
		return nil, sql.ErrNoRows
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT t.id, t.trip_number, COALESCE(t.booking_id,''), t.departure_time, t.status,
		       COALESCE(d.first_name,''), COALESCE(d.last_name,''),
		       COALESCE(v.vehicle_number,''), COALESCE(r.source,''), COALESCE(r.destination,'')
		FROM trips t
		LEFT JOIN drivers d ON d.id = t.driver_id
		LEFT JOIN vehicles v ON v.id = t.vehicle_id
		LEFT JOIN routes r ON r.id = t.route_id
		WHERE t.trip_number = ? AND t.tenant_id = ? LIMIT 1`, number, string(tenantID))
	var d AgentTripDetail
	var departure time.Time
	var first, last, source, dest string
	if err := row.Scan(&d.ID, &d.Number, &d.BookingID, &departure, &d.Status, &first, &last, &d.Vehicle, &source, &dest); err != nil {
		return nil, err
	}
	d.Driver = strings.TrimSpace(first + " " + last)
	d.Route = source + " -> " + dest
	d.Departure = departure.Format("2006-01-02 15:04")
	return &d, nil
}
