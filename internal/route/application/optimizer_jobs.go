package application

// Route-optimization job persistence + read model, extracted from
// internal/handlers/routes.go (handlers-end-state ADR, Phase 2). The handler
// keeps HTTP/JSON parsing, optimizer input validation (optimizer.Validate,
// max-50-shipments cap), provider selection/config and rendering; this file
// keeps the exact route_optimization_jobs SQL, tenant scoping and the legacy
// best-effort status-update semantics (Spec 18 Wave A).
//
// Tenant scoping: every job statement filters tenant_id (create/list/get),
// so a foreign-tenant job id reads as not-found; the optimizer vehicle lookup
// is tenant-scoped, excludes blocked vehicles and keeps the legacy
// ordering/limit. The route master-data CRUD paths stay on the legacy
// service (h.Services.Routes) - only this file's raw SQL moved.

import (
	"context"
	"database/sql"

	"github.com/google/uuid"
)

// OptimizationVehicle is one selectable fleet vehicle on the optimize page.
type OptimizationVehicle struct {
	ID                 string
	RegistrationNumber string
	VehicleNumber      string
	Model              string
	Capacity           int64
}

// OptimizationJobRow is one row of the recent-jobs fragment/API list.
type OptimizationJobRow struct {
	ID          string  `json:"id"`
	Status      string  `json:"status"`
	Provider    string  `json:"provider"`
	CreatedAt   string  `json:"created_at"`
	CompletedAt *string `json:"completed_at,omitempty"`
}

// OptimizationJobDetail is the polling view of one job. The sql.NullString
// fields preserve the legacy absent-vs-empty JSON semantics.
type OptimizationJobDetail struct {
	ID           string
	Status       string
	Provider     string
	CreatedAt    string
	ResultJSON   sql.NullString
	ErrorMessage sql.NullString
	CompletedAt  sql.NullString
}

// RouteOptimizationService is the persistence/read facade for route
// optimization jobs. It is stateless and cheap to construct per request.
type RouteOptimizationService struct {
	db *sql.DB
}

// NewRouteOptimizationService wires the service against the database.
func NewRouteOptimizationService(db *sql.DB) *RouteOptimizationService {
	return &RouteOptimizationService{db: db}
}

// VehiclesForOptimization returns up to 50 non-blocked tenant vehicles for
// the optimizer dropdown. Legacy semantics: a query/scan error renders an
// empty list instead of failing the page.
func (s *RouteOptimizationService) VehiclesForOptimization(ctx context.Context, tenantID string) []OptimizationVehicle {
	vehicles := []OptimizationVehicle{}
	if s.db == nil {
		return vehicles
	}
	if rows, err := s.db.QueryContext(ctx, `
		SELECT id, registration_number, vehicle_number, COALESCE(model, ''), capacity
		FROM vehicles
		WHERE tenant_id = $1 AND status != 'blocked'
		ORDER BY registration_number ASC LIMIT 50`, tenantID); err == nil {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var vehicle OptimizationVehicle
			if rows.Scan(&vehicle.ID, &vehicle.RegistrationNumber, &vehicle.VehicleNumber, &vehicle.Model, &vehicle.Capacity) == nil {
				vehicles = append(vehicles, vehicle)
			}
		}
	}
	return vehicles
}

// CreateJob inserts one pending route-optimization job and returns its id.
func (s *RouteOptimizationService) CreateJob(ctx context.Context, tenantID, inputJSON, provider, createdBy string) (string, error) {
	jobID := uuid.NewString()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO route_optimization_jobs (id, tenant_id, input_json, status, provider, created_by) VALUES ($1, $2, $3, 'pending', $4, $5)`,
		jobID, tenantID, inputJSON, provider, createdBy)
	if err != nil {
		return "", err
	}
	return jobID, nil
}

// MarkProcessing flips the job to processing. Best-effort like the legacy
// handler, which ignored the update error.
func (s *RouteOptimizationService) MarkProcessing(ctx context.Context, jobID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE route_optimization_jobs SET status='processing' WHERE id=$1`, jobID)
	return err
}

// MarkFailed records the solver error + completion timestamp. Best-effort.
func (s *RouteOptimizationService) MarkFailed(ctx context.Context, jobID, message string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE route_optimization_jobs SET status='failed', error_message=$1, completed_at=CURRENT_TIMESTAMP WHERE id=$2`,
		message, jobID)
	return err
}

// MarkCompleted records the solver result + completion timestamp. Best-effort.
func (s *RouteOptimizationService) MarkCompleted(ctx context.Context, jobID, resultJSON string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE route_optimization_jobs SET status='completed', result_json=$1, completed_at=CURRENT_TIMESTAMP WHERE id=$2`,
		resultJSON, jobID)
	return err
}

// RecentJobs returns the tenant's 20 newest jobs. Legacy semantics: scan
// failures were assigned into zero values but the row was still appended,
// and rows.Err is not checked.
func (s *RouteOptimizationService) RecentJobs(ctx context.Context, tenantID string) ([]OptimizationJobRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, status, provider, created_at, completed_at FROM route_optimization_jobs WHERE tenant_id=$1 ORDER BY created_at DESC LIMIT 20`, tenantID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var jobs []OptimizationJobRow
	for rows.Next() {
		var job OptimizationJobRow
		var created, completed sql.NullString
		_ = rows.Scan(&job.ID, &job.Status, &job.Provider, &created, &completed)
		job.CreatedAt = created.String
		if completed.Valid {
			job.CompletedAt = &completed.String
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

// Job returns one tenant-scoped job for polling. sql.ErrNoRows surfaces for
// unknown ids and foreign-tenant ids (fail-closed); the handler maps every
// error to 404 like the legacy code.
func (s *RouteOptimizationService) Job(ctx context.Context, jobID, tenantID string) (OptimizationJobDetail, error) {
	var id, status, provider, inputJSON, resultJSON, errMsg, createdAt, completedAt sql.NullString
	var detail OptimizationJobDetail
	err := s.db.QueryRowContext(ctx,
		`SELECT id, status, provider, input_json, result_json, error_message, created_at, completed_at FROM route_optimization_jobs WHERE id=$1 AND tenant_id=$2`, jobID, tenantID).
		Scan(&id, &status, &provider, &inputJSON, &resultJSON, &errMsg, &createdAt, &completedAt)
	if err != nil {
		return detail, err
	}
	detail = OptimizationJobDetail{
		ID:           id.String,
		Status:       status.String,
		Provider:     provider.String,
		CreatedAt:    createdAt.String,
		ResultJSON:   resultJSON,
		ErrorMessage: errMsg,
		CompletedAt:  completedAt,
	}
	return detail, nil
}
