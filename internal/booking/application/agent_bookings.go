package application

// Agent-facing booking facade, extracted from internal/agent/tools.go
// (handlers-end-state ADR, Phase 3). Create delegates to the clean
// CreateBookingUseCase (validation/lifecycle/tenant gates); the read model is
// a tenant-scoped by-id/by-number query for the tool JSON.

import (
	"context"
	"database/sql"
	"time"

	"transport-app/internal/shared"
	"transport-app/internal/shared/clock"
	"transport-app/internal/shared/id"
	"transport-app/internal/shared/uow"
)

// AgentBookingView is the booking detail read model (create + get by number).
type AgentBookingView struct {
	ID          string
	Number      string
	Customer    string
	Route       string
	PickupDate  time.Time
	VehicleType string
	Passengers  int64
	Price       float64
	Status      string
}

// AgentCreateBookingInput carries the already-parsed/validated tool args
// (PickupDate is the legacy RFC3339 string the clean command expects).
type AgentCreateBookingInput struct {
	CustomerID  string
	RouteID     string
	PickupDate  string
	VehicleType string
	Passengers  int64
	Price       float64
	Notes       string
}

// AgentBookingService is the booking facade. Stateless.
type AgentBookingService struct{ db *sql.DB }

// NewAgentBookingService wires the facade against the database.
func NewAgentBookingService(db *sql.DB) *AgentBookingService { return &AgentBookingService{db: db} }

const agentBookingSelect = `
	SELECT b.id, b.booking_number, c.name, r.source, r.destination, b.pickup_date,
	       b.vehicle_type, b.passengers, b.price, b.status
	FROM bookings b
	JOIN customers c ON c.id = b.customer_id
	JOIN routes r ON r.id = b.route_id`

// Create runs the owning-slice creation rules and returns the read model.
func (s *AgentBookingService) Create(ctx context.Context, tenantID shared.TenantID, in AgentCreateBookingInput) (*AgentBookingView, error) {
	if s == nil || s.db == nil {
		return nil, sql.ErrNoRows
	}
	uc := NewCreateBookingUseCase(uow.NewSQLUnitOfWork(s.db), id.NewUUIDGenerator(), clock.NewRealClock())
	bID, err := uc.Execute(ctx, CreateBookingCommand{
		TenantID: tenantID, CustomerID: in.CustomerID, RouteID: in.RouteID,
		PickupDate: in.PickupDate, VehicleType: in.VehicleType,
		Passengers: in.Passengers, Price: in.Price, Notes: in.Notes,
	})
	if err != nil {
		return nil, err
	}
	return s.getByQuery(ctx, tenantID, `b.id = ?`, string(bID))
}

// GetByNumber returns the tenant's booking by booking number.
func (s *AgentBookingService) GetByNumber(ctx context.Context, tenantID shared.TenantID, number string) (*AgentBookingView, error) {
	return s.getByQuery(ctx, tenantID, `b.booking_number = ?`, number)
}

func (s *AgentBookingService) getByQuery(ctx context.Context, tenantID shared.TenantID, predicate, arg string) (*AgentBookingView, error) {
	if s == nil || s.db == nil {
		return nil, sql.ErrNoRows
	}
	row := s.db.QueryRowContext(ctx, agentBookingSelect+` WHERE b.tenant_id = ? AND `+predicate+` LIMIT 1`, string(tenantID), arg)
	var v AgentBookingView
	var source, dest string
	if err := row.Scan(&v.ID, &v.Number, &v.Customer, &source, &dest, &v.PickupDate, &v.VehicleType, &v.Passengers, &v.Price, &v.Status); err != nil {
		return nil, err
	}
	v.Route = source + " -> " + dest
	return &v, nil
}
