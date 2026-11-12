package maintenance

// Work-order application facade behind the agent work-order tools, extracted
// from internal/agent/tools.go (handlers-end-state ADR, Phase 3). The tools
// keep their JSON schemas, argument validation/parsing, permission and
// approval gates and model-facing strings; this file owns the store wiring,
// tenant derivation, creation defaults and the done-transition side effects
// (service-record write + due-flag resolution) so tools never see the SQL
// repository.

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"

	"transport-app/internal/maintenance/domain"
	maintsql "transport-app/internal/maintenance/infrastructure/sql"
	"transport-app/internal/shared"
)

// ErrStoreUnavailable is returned when no database is wired (legacy tool
// string "maintenance store unavailable").
var ErrStoreUnavailable = errors.New("maintenance store unavailable")

// WorkOrderFacade is the application seam for job cards. It is stateless and
// cheap to construct per request.
type WorkOrderFacade struct {
	db   *sql.DB
	repo *maintsql.MaintenanceRepository
}

// NewWorkOrderFacade wires the facade against the database; nil DB yields a
// nil facade so callers fail closed with ErrStoreUnavailable.
func NewWorkOrderFacade(db *sql.DB) *WorkOrderFacade {
	if db == nil {
		return nil
	}
	return &WorkOrderFacade{db: db, repo: maintsql.NewMaintenanceRepository(db)}
}

// CreateWorkOrderInput carries the already-parsed/validated tool arguments.
type CreateWorkOrderInput struct {
	VehicleID    string
	Title        string
	Description  string
	Assignee     string
	Vendor       string
	CostEstimate *float64
	ScheduleID   *string
	DueAt        *time.Time
}

// Create opens a job card in the acting tenant's store with legacy defaults
// (uuid id, status open).
func (f *WorkOrderFacade) Create(ctx context.Context, in CreateWorkOrderInput) (*domain.WorkOrder, error) {
	if f == nil {
		return nil, ErrStoreUnavailable
	}
	wo := &domain.WorkOrder{
		ID:           uuid.NewString(),
		TenantID:     tenantOf(ctx),
		VehicleID:    in.VehicleID,
		ScheduleID:   in.ScheduleID,
		Title:        in.Title,
		Description:  in.Description,
		Assignee:     in.Assignee,
		Vendor:       in.Vendor,
		CostEstimate: in.CostEstimate,
		Status:       domain.WorkOrderOpen,
		DueAt:        in.DueAt,
	}
	if err := f.repo.CreateWorkOrder(ctx, *wo); err != nil {
		return nil, err
	}
	return wo, nil
}

// List returns the acting tenant's job cards, optionally status-filtered.
func (f *WorkOrderFacade) List(ctx context.Context, status string, limit int) ([]domain.WorkOrder, error) {
	if f == nil {
		return nil, ErrStoreUnavailable
	}
	return f.repo.ListWorkOrders(ctx, tenantOf(ctx), status, limit)
}

// Get returns one card of the acting tenant, nil when absent/foreign.
func (f *WorkOrderFacade) Get(ctx context.Context, id string) (*domain.WorkOrder, error) {
	if f == nil {
		return nil, ErrStoreUnavailable
	}
	return f.repo.FindWorkOrder(ctx, tenantOf(ctx), id)
}

// TransitionWorkOrderInput carries the validated transition request.
type TransitionWorkOrderInput struct {
	ID       string
	Status   string
	Assignee string
	Vendor   string
	Actor    string
}

// TransitionWorkOrderResult reports the applied transition.
type TransitionWorkOrderResult struct {
	ID        string
	VehicleID string
	Status    string
	Completed bool
}

// Transition applies an assignment (optional) and the status move. Closing as
// done routes through CompleteWorkOrder so the service record is written, then
// re-runs due-flag resolution exactly like the legacy tool path.
func (f *WorkOrderFacade) Transition(ctx context.Context, in TransitionWorkOrderInput) (*TransitionWorkOrderResult, error) {
	if f == nil {
		return nil, ErrStoreUnavailable
	}
	tenant := tenantOf(ctx)
	if in.Assignee != "" || in.Vendor != "" {
		if err := f.repo.AssignWorkOrder(ctx, tenant, in.ID, in.Assignee, in.Vendor); err != nil {
			return nil, err
		}
	}
	if in.Status == domain.WorkOrderDone {
		done, err := f.repo.CompleteWorkOrder(ctx, tenant, in.ID, in.Actor)
		if err != nil {
			return nil, err
		}
		NewWorker(f.db, nil, nil, 0, "").EvaluateResolution(ctx, done.VehicleID)
		return &TransitionWorkOrderResult{ID: done.ID, VehicleID: done.VehicleID, Status: done.Status, Completed: true}, nil
	}
	if err := f.repo.TransitionWorkOrder(ctx, tenant, in.ID, in.Status); err != nil {
		return nil, err
	}
	return &TransitionWorkOrderResult{ID: in.ID, Status: in.Status}, nil
}

func tenantOf(ctx context.Context) string {
	return string(shared.TenantIDFromContext(ctx))
}
