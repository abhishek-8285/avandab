package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"time"

	"transport-app/internal/events"
	"transport-app/internal/shared"
)

// ExperimentsService implements the A/B experiment framework (Spec 16 §5):
// experiment lifecycle management, deterministic variant assignment, traffic
// splitting, feature flag evaluation, and metric aggregation.
type ExperimentsService struct {
	baseService
	db    *sql.DB
	audit *FounderAuditService
}

// NewExperimentsService constructs an ExperimentsService with raw DB access.
func NewExperimentsService(bs baseService, db *sql.DB) *ExperimentsService {
	return &ExperimentsService{baseService: bs, db: db}
}

// NewExperimentsServiceForTest builds a service with a custom event bus for tests.
func NewExperimentsServiceForTest(db *sql.DB, bus events.EventBus) *ExperimentsService {
	return &ExperimentsService{baseService: baseService{events: bus}, db: db}
}

// SetAudit wires the founder audit service used to record experiment lifecycle events.
func (s *ExperimentsService) SetAudit(a *FounderAuditService) {
	s.audit = a
}

// recordAudit is a nil-safe audit helper for experiment lifecycle transitions.
func (s *ExperimentsService) recordAudit(ctx context.Context, action, experimentID, detail string) {
	if s.audit == nil {
		return
	}
	_ = s.audit.RecordAudit(ctx, AuditEntry{
		TenantID:     string(shared.TenantIDFromContext(ctx)),
		ActorID:      "system",
		ActorRole:    "admin",
		Action:       action,
		ResourceType: "experiment",
		ResourceID:   experimentID,
		Details:      detail,
	})
}

// CountByStatus returns the number of experiments in a given status for a tenant.
func (s *ExperimentsService) CountByStatus(ctx context.Context, tenantID, status string) (int, error) {
	if s.db == nil {
		return 0, fmt.Errorf("database unavailable")
	}
	tid, err := shared.RequireTenantOr(ctx, tenantID)
	if err != nil {
		return 0, err
	}
	tenantID = string(tid)
	var n int
	err = s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM experiments_spec16 WHERE tenant_id = $1 AND status = $2`, tenantID, status).Scan(&n)
	return n, err
}

// Status constants matching CHECK constraint in 00058.
const (
	ExperimentStatusDraft     = "draft"
	ExperimentStatusRunning   = "running"
	ExperimentStatusPaused    = "paused"
	ExperimentStatusCompleted = "completed"
	ExperimentStatusArchived  = "archived"
)

// Subject types matching CHECK constraint in 00058.
const (
	SubjectTypeUser     = "user"
	SubjectTypeDriver   = "driver"
	SubjectTypeVehicle  = "vehicle"
	SubjectTypeCustomer = "customer"
)

// Variant identifiers.
const (
	VariantA = "a" // control
	VariantB = "b" // treatment
)

// Experiment is a row in experiments_spec16 (Spec 16 §5).
type Experiment struct {
	ID           string
	TenantID     string
	Name         string
	Description  string
	VariantA     string // control label
	VariantB     string // treatment label
	TrafficSplit float64
	Status       string
	StartDate    *time.Time
	EndDate      *time.Time
	MetricName   string
	CreatedBy    string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// ExperimentAssignment is a row in experiment_assignments.
type ExperimentAssignment struct {
	ID           string
	ExperimentID string
	TenantID     string
	SubjectType  string
	SubjectID    string
	Variant      string
	AssignedAt   time.Time
}

// CreateExperiment creates a new experiment in draft status.
func (s *ExperimentsService) CreateExperiment(ctx context.Context, exp Experiment) (string, error) {
	if s.db == nil {
		return "", fmt.Errorf("database unavailable")
	}
	now := time.Now().UTC()

	if tid, err := shared.RequireTenantOr(ctx, exp.TenantID); err != nil {
		return "", err
	} else {
		exp.TenantID = string(tid)
	}
	if exp.TrafficSplit < 0 || exp.TrafficSplit > 100 {
		return "", fmt.Errorf("traffic_split must be between 0 and 100, got %f", exp.TrafficSplit)
	}
	if exp.VariantA == "" {
		exp.VariantA = "control"
	}
	if exp.VariantB == "" {
		exp.VariantB = "treatment"
	}

	id := generateDisplayID("exp")
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO experiments_spec16
		 (id, tenant_id, name, description, variant_a, variant_b, traffic_split,
		  status, start_date, end_date, metric_name, created_by, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, 'draft', $8, $9, $10, $11, $12, $13)`,
		id, exp.TenantID, exp.Name, exp.Description, exp.VariantA, exp.VariantB,
		exp.TrafficSplit, exp.StartDate, exp.EndDate, exp.MetricName, exp.CreatedBy, now, now)
	if err != nil {
		return "", fmt.Errorf("insert experiment: %w", err)
	}
	s.recordAudit(ctx, AuditActionExperimentCreate, id, fmt.Sprintf(`{"name":%q}`, exp.Name))
	return id, nil
}

// StartExperiment transitions an experiment from draft to running.
func (s *ExperimentsService) StartExperiment(ctx context.Context, experimentID string) error {
	if s.db == nil {
		return fmt.Errorf("database unavailable")
	}
	result, err := s.db.ExecContext(ctx,
		`UPDATE experiments_spec16 SET status = 'running', updated_at = $1
		 WHERE id = $2 AND status = 'draft'`,
		time.Now().UTC(), experimentID)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("experiment %s not found or not in draft status", experimentID)
	}
	s.recordAudit(ctx, AuditActionExperimentStart, experimentID, "{}")
	return nil
}

// PauseExperiment transitions running → paused.
func (s *ExperimentsService) PauseExperiment(ctx context.Context, experimentID string) error {
	if s.db == nil {
		return fmt.Errorf("database unavailable")
	}
	result, err := s.db.ExecContext(ctx,
		`UPDATE experiments_spec16 SET status = 'paused', updated_at = $1
		 WHERE id = $2 AND status = 'running'`,
		time.Now().UTC(), experimentID)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("experiment %s not found or not running", experimentID)
	}
	return nil
}

// ResumeExperiment transitions paused → running.
func (s *ExperimentsService) ResumeExperiment(ctx context.Context, experimentID string) error {
	if s.db == nil {
		return fmt.Errorf("database unavailable")
	}
	result, err := s.db.ExecContext(ctx,
		`UPDATE experiments_spec16 SET status = 'running', updated_at = $1
		 WHERE id = $2 AND status = 'paused'`,
		time.Now().UTC(), experimentID)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("experiment %s not found or not paused", experimentID)
	}
	return nil
}

// CompleteExperiment transitions running/paused → completed.
func (s *ExperimentsService) CompleteExperiment(ctx context.Context, experimentID string) error {
	if s.db == nil {
		return fmt.Errorf("database unavailable")
	}
	result, err := s.db.ExecContext(ctx,
		`UPDATE experiments_spec16 SET status = 'completed', updated_at = $1
		 WHERE id = $2 AND status IN ('running', 'paused')`,
		time.Now().UTC(), experimentID)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("experiment %s not found or not in running/paused status", experimentID)
	}
	s.recordAudit(ctx, AuditActionExperimentComplete, experimentID, "{}")
	return nil
}

// ArchiveExperiment transitions completed → archived.
func (s *ExperimentsService) ArchiveExperiment(ctx context.Context, experimentID string) error {
	if s.db == nil {
		return fmt.Errorf("database unavailable")
	}
	result, err := s.db.ExecContext(ctx,
		`UPDATE experiments_spec16 SET status = 'archived', updated_at = $1
		 WHERE id = $2 AND status = 'completed'`,
		time.Now().UTC(), experimentID)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("experiment %s not found or not completed", experimentID)
	}
	return nil
}

// GetExperiment returns a single experiment by ID.
func (s *ExperimentsService) GetExperiment(ctx context.Context, experimentID string) (*Experiment, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database unavailable")
	}
	var exp Experiment
	var startDate, endDate sql.NullTime
	err := s.db.QueryRowContext(ctx,
		`SELECT id, tenant_id, name, description, variant_a, variant_b, traffic_split,
		        status, start_date, end_date, metric_name, created_by, created_at, updated_at
		 FROM experiments_spec16 WHERE id = $1`, experimentID).
		Scan(&exp.ID, &exp.TenantID, &exp.Name, &exp.Description, &exp.VariantA, &exp.VariantB,
			&exp.TrafficSplit, &exp.Status, &startDate, &endDate, &exp.MetricName,
			&exp.CreatedBy, &exp.CreatedAt, &exp.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("experiment not found")
	}
	if err != nil {
		return nil, err
	}
	if startDate.Valid {
		exp.StartDate = &startDate.Time
	}
	if endDate.Valid {
		exp.EndDate = &endDate.Time
	}
	return &exp, nil
}

// GetExperimentByName returns a single experiment by tenant + human-readable name.
func (s *ExperimentsService) GetExperimentByName(ctx context.Context, tenantID, name string) (*Experiment, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database unavailable")
	}
	var exp Experiment
	var startDate, endDate sql.NullTime
	err := s.db.QueryRowContext(ctx,
		`SELECT id, tenant_id, name, description, variant_a, variant_b, traffic_split,
		        status, start_date, end_date, metric_name, created_by, created_at, updated_at
		 FROM experiments_spec16 WHERE tenant_id = $1 AND name = $2`, tenantID, name).
		Scan(&exp.ID, &exp.TenantID, &exp.Name, &exp.Description, &exp.VariantA, &exp.VariantB,
			&exp.TrafficSplit, &exp.Status, &startDate, &endDate, &exp.MetricName,
			&exp.CreatedBy, &exp.CreatedAt, &exp.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("experiment not found")
	}
	if err != nil {
		return nil, err
	}
	if startDate.Valid {
		exp.StartDate = &startDate.Time
	}
	if endDate.Valid {
		exp.EndDate = &endDate.Time
	}
	return &exp, nil
}

// ListExperiments returns experiments with optional status filter.
func (s *ExperimentsService) ListExperiments(ctx context.Context, tenantID string, status string) ([]Experiment, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database unavailable")
	}
	tid, err := shared.RequireTenantOr(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	tenantID = string(tid)
	query := `SELECT id, tenant_id, name, description, variant_a, variant_b, traffic_split,
	          status, start_date, end_date, metric_name, created_by, created_at, updated_at
	          FROM experiments_spec16 WHERE tenant_id = $1`
	args := []interface{}{tenantID}

	if status != "" {
		query += " AND status = ?"
		args = append(args, status)
	}
	query += " ORDER BY created_at DESC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var experiments []Experiment
	for rows.Next() {
		var exp Experiment
		var startDate, endDate sql.NullTime
		if err := rows.Scan(&exp.ID, &exp.TenantID, &exp.Name, &exp.Description,
			&exp.VariantA, &exp.VariantB, &exp.TrafficSplit, &exp.Status,
			&startDate, &endDate, &exp.MetricName, &exp.CreatedBy,
			&exp.CreatedAt, &exp.UpdatedAt); err != nil {
			return nil, err
		}
		if startDate.Valid {
			exp.StartDate = &startDate.Time
		}
		if endDate.Valid {
			exp.EndDate = &endDate.Time
		}
		experiments = append(experiments, exp)
	}
	return experiments, rows.Err()
}

// AssignVariant deterministically assigns a subject to a variant for an experiment.
// Idempotent: an existing assignment is returned unchanged. Only running
// experiments within their date bounds may assign. Uses FNV-1a over
// (experimentID + subjectType + subjectID) so each experiment randomizes a
// subject independently and consistently across calls.
func (s *ExperimentsService) AssignVariant(ctx context.Context, tenantID, experimentID, subjectType, subjectID string) (string, error) {
	if s.db == nil {
		return "", fmt.Errorf("database unavailable")
	}

	// 1. Idempotent: return existing assignment if present.
	var existingVariant string
	err := s.db.QueryRowContext(ctx,
		`SELECT variant FROM experiment_assignments
		 WHERE experiment_id = $1 AND subject_type = $2 AND subject_id = $3`,
		experimentID, subjectType, subjectID).Scan(&existingVariant)
	if err == nil {
		return existingVariant, nil
	}
	if err != sql.ErrNoRows {
		return "", fmt.Errorf("assignment lookup failed: %w", err)
	}

	// 2. Load experiment to verify status + traffic split.
	exp, err := s.GetExperiment(ctx, experimentID)
	if err != nil {
		return "", err
	}
	if exp.Status != ExperimentStatusRunning {
		return "", fmt.Errorf("experiment %s is not running (status: %s)", experimentID, exp.Status)
	}

	// 3. Date bounds.
	now := time.Now()
	if exp.StartDate != nil && now.Before(*exp.StartDate) {
		return "", fmt.Errorf("experiment %s has not started yet", experimentID)
	}
	if exp.EndDate != nil && now.After(*exp.EndDate) {
		return "", fmt.Errorf("experiment %s has ended", experimentID)
	}

	// 4. Deterministic hash-based assignment.
	variant := computeVariant(experimentID, subjectType, subjectID, exp.TrafficSplit)

	// 5. Persist assignment (UNIQUE constraint guards idempotency).
	assignmentID := generateDisplayID("expa")
	if _, err = s.db.ExecContext(ctx,
		`INSERT INTO experiment_assignments (id, experiment_id, tenant_id, subject_type, subject_id, variant)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		assignmentID, experimentID, tenantID, subjectType, subjectID, variant); err != nil {
		// Concurrent insert race: fall back to reading the winner.
		if rerr := s.db.QueryRowContext(ctx,
			`SELECT variant FROM experiment_assignments
			 WHERE experiment_id = $1 AND subject_type = $2 AND subject_id = $3`,
			experimentID, subjectType, subjectID).Scan(&existingVariant); rerr == nil {
			return existingVariant, nil
		}
		return "", fmt.Errorf("failed to persist assignment: %w", err)
	}

	return variant, nil
}

// computeVariant uses FNV-1a hash to deterministically assign a subject to a
// variant. Hash input order is fixed and MUST NOT change after deployment.
func computeVariant(experimentID, subjectType, subjectID string, trafficSplit float64) string {
	h := fnv.New32a()
	h.Write([]byte(experimentID))
	h.Write([]byte(subjectType))
	h.Write([]byte(subjectID))
	hashValue := h.Sum32()

	percentage := float64(hashValue%10000) / 100.0 // 0.00 .. 99.99

	if percentage < trafficSplit {
		return VariantB
	}
	return VariantA
}

// GetAssignment returns the existing assignment variant, or "" if not assigned.
func (s *ExperimentsService) GetAssignment(ctx context.Context, experimentID, subjectType, subjectID string) (string, error) {
	if s.db == nil {
		return "", fmt.Errorf("database unavailable")
	}
	var variant string
	err := s.db.QueryRowContext(ctx,
		`SELECT variant FROM experiment_assignments
		 WHERE experiment_id = $1 AND subject_type = $2 AND subject_id = $3`,
		experimentID, subjectType, subjectID).Scan(&variant)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return variant, err
}

// ListAssignments returns all assignments for an experiment.
func (s *ExperimentsService) ListAssignments(ctx context.Context, experimentID string) ([]ExperimentAssignment, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database unavailable")
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, experiment_id, tenant_id, subject_type, subject_id, variant, assigned_at
		 FROM experiment_assignments WHERE experiment_id = $1 ORDER BY assigned_at DESC`,
		experimentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ExperimentAssignment
	for rows.Next() {
		var a ExperimentAssignment
		if err := rows.Scan(&a.ID, &a.ExperimentID, &a.TenantID, &a.SubjectType,
			&a.SubjectID, &a.Variant, &a.AssignedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// EvaluateFeatureFlag is the primary API for other services. Returns the
// variant for the subject (by experiment name), defaulting to control ("a")
// when the experiment is missing, not running, or assignment fails.
func (s *ExperimentsService) EvaluateFeatureFlag(ctx context.Context, tenantID, experimentName, subjectType, subjectID string) string {
	if tenantID == "" {
		// Safe default with no data leak: unknown tenant gets control.
		tenantID = string(shared.TenantIDFromContext(ctx))
	}
	if tenantID == "" {
		return VariantA
	}
	exp, err := s.GetExperimentByName(ctx, tenantID, experimentName)
	if err != nil || exp == nil {
		return VariantA
	}
	if exp.Status != ExperimentStatusRunning {
		return VariantA
	}
	variant, err := s.AssignVariant(ctx, tenantID, exp.ID, subjectType, subjectID)
	if err != nil {
		return VariantA
	}
	return variant
}

// IsInTreatment returns true when the subject is in the treatment variant ("b").
func (s *ExperimentsService) IsInTreatment(ctx context.Context, tenantID, experimentName, subjectType, subjectID string) bool {
	return s.EvaluateFeatureFlag(ctx, tenantID, experimentName, subjectType, subjectID) == VariantB
}

// experimentMetricDetail is the JSON payload stored in founder_audit for metrics.
type experimentMetricDetail struct {
	Variant     string  `json:"variant"`
	MetricValue float64 `json:"metric_value"`
	SubjectType string  `json:"subject_type"`
	SubjectID   string  `json:"subject_id"`
}

// RecordMetric records a metric value for an already-assigned subject.
func (s *ExperimentsService) RecordMetric(ctx context.Context, tenantID, experimentID, subjectType, subjectID string, metricValue float64) error {
	if s.db == nil {
		return fmt.Errorf("database unavailable")
	}
	variant, err := s.GetAssignment(ctx, experimentID, subjectType, subjectID)
	if err != nil {
		return err
	}
	if variant == "" {
		return fmt.Errorf("subject not assigned to experiment %s", experimentID)
	}

	detail := experimentMetricDetail{
		Variant:     variant,
		MetricValue: metricValue,
		SubjectType: subjectType,
		SubjectID:   subjectID,
	}
	payload, err := json.Marshal(detail)
	if err != nil {
		return fmt.Errorf("marshal metric detail: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO founder_audit (id, tenant_id, actor_id, actor_role, action, resource_type, resource_id, details)
		 VALUES ($1, $2, $3, 'system', 'experiment_metric', 'experiment', $4, $5)`,
		generateDisplayID("fa"), tenantID, subjectID, experimentID, string(payload))
	if err != nil {
		return fmt.Errorf("insert experiment metric: %w", err)
	}
	return nil
}

// VariantMetrics holds aggregate stats for one variant.
type VariantMetrics struct {
	Variant   string
	Count     int
	AvgMetric float64
	MinMetric float64
	MaxMetric float64
}

// ExperimentResults holds aggregated metrics per variant for an experiment.
type ExperimentResults struct {
	ExperimentID  string
	VariantA      VariantMetrics
	VariantB      VariantMetrics
	TotalSubjects int
}

// GetExperimentResults aggregates recorded metrics per variant from founder_audit.
func (s *ExperimentsService) GetExperimentResults(ctx context.Context, experimentID string) (*ExperimentResults, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database unavailable")
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT details FROM founder_audit
		 WHERE resource_type = 'experiment' AND resource_id = $1 AND action = 'experiment_metric'
		 ORDER BY created_at ASC`,
		experimentID)
	if err != nil {
		return nil, fmt.Errorf("query experiment results: %w", err)
	}
	defer rows.Close()

	results := &ExperimentResults{
		ExperimentID: experimentID,
		VariantA:     VariantMetrics{Variant: VariantA},
		VariantB:     VariantMetrics{Variant: VariantB},
	}
	sumA, sumB := 0.0, 0.0
	for rows.Next() {
		var details string
		if err := rows.Scan(&details); err != nil {
			return nil, err
		}
		var d experimentMetricDetail
		if err := json.Unmarshal([]byte(details), &d); err != nil {
			continue
		}
		switch d.Variant {
		case VariantA:
			results.VariantA.Count++
			sumA += d.MetricValue
			if results.VariantA.Count == 1 || d.MetricValue < results.VariantA.MinMetric {
				results.VariantA.MinMetric = d.MetricValue
			}
			if d.MetricValue > results.VariantA.MaxMetric {
				results.VariantA.MaxMetric = d.MetricValue
			}
		case VariantB:
			results.VariantB.Count++
			sumB += d.MetricValue
			if results.VariantB.Count == 1 || d.MetricValue < results.VariantB.MinMetric {
				results.VariantB.MinMetric = d.MetricValue
			}
			if d.MetricValue > results.VariantB.MaxMetric {
				results.VariantB.MaxMetric = d.MetricValue
			}
		}
	}
	if results.VariantA.Count > 0 {
		results.VariantA.AvgMetric = sumA / float64(results.VariantA.Count)
	}
	if results.VariantB.Count > 0 {
		results.VariantB.AvgMetric = sumB / float64(results.VariantB.Count)
	}
	results.TotalSubjects = results.VariantA.Count + results.VariantB.Count
	return results, rows.Err()
}
