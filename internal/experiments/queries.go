package experiments

// Read-only experiments facade for the founder handlers, extracted from the
// legacy service container wiring (handlers-end-state ADR, Phase 4 increment
// 3). The experimental assignment engine stays in this slice; this file owns
// the tenant-scoped spec16 read queries.

import (
	"context"
	"database/sql"
	"time"

	"transport-app/internal/shared"
)

// Experiment is one experiments_spec16 row (legacy-compatible fields).
type Experiment struct {
	ID           string
	TenantID     string
	Name         string
	Description  string
	VariantA     string
	VariantB     string
	TrafficSplit float64
	Status       string
	StartDate    *time.Time
	EndDate      *time.Time
	MetricName   string
	CreatedBy    string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Status constants matching the 00058 CHECK constraint.
const (
	ExperimentStatusDraft     = "draft"
	ExperimentStatusRunning   = "running"
	ExperimentStatusPaused    = "paused"
	ExperimentStatusCompleted = "completed"
	ExperimentStatusArchived  = "archived"
)

// Queries is the experiments read facade. Stateless.
type Queries struct{ db *sql.DB }

// NewQueries wires the facade against the database.
func NewQueries(db *sql.DB) *Queries { return &Queries{db: db} }

// CountByStatus counts the tenant's experiments in a status.
func (q *Queries) CountByStatus(ctx context.Context, tenantID, status string) (int, error) {
	if q == nil || q.db == nil {
		return 0, sql.ErrNoRows
	}
	tid, err := shared.RequireTenantOr(ctx, tenantID)
	if err != nil {
		return 0, err
	}
	var n int
	err = q.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM experiments_spec16 WHERE tenant_id = $1 AND status = $2`, string(tid), status).Scan(&n)
	return n, err
}

// ListExperiments returns the tenant's experiments, newest first, optionally
// filtered by status (legacy list shape).
func (q *Queries) ListExperiments(ctx context.Context, tenantID, status string) ([]Experiment, error) {
	if q == nil || q.db == nil {
		return nil, sql.ErrNoRows
	}
	tid, err := shared.RequireTenantOr(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	query := `SELECT id, tenant_id, name, description, variant_a, variant_b, traffic_split,
	          status, start_date, end_date, metric_name, created_by, created_at, updated_at
	          FROM experiments_spec16 WHERE tenant_id = $1`
	args := []any{string(tid)}
	if status != "" {
		query += " AND status = ?"
		args = append(args, status)
	}
	query += " ORDER BY created_at DESC"
	rows, err := q.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Experiment
	for rows.Next() {
		var e Experiment
		var start, end sql.NullTime
		if err := rows.Scan(&e.ID, &e.TenantID, &e.Name, &e.Description, &e.VariantA, &e.VariantB, &e.TrafficSplit,
			&e.Status, &start, &end, &e.MetricName, &e.CreatedBy, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		if start.Valid {
			e.StartDate = &start.Time
		}
		if end.Valid {
			e.EndDate = &end.Time
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
