package service

import (
	"context"
	"database/sql"
	"fmt"
	"time"
	"transport-app/internal/shared"
)

// FounderAuditService implements the append-only founder audit trail (Spec 16 §7).
// Once an entry is written it is never updated or deleted.
type FounderAuditService struct {
	baseService
	db *sql.DB
}

// NewFounderAuditService constructs a FounderAuditService.
func NewFounderAuditService(bs baseService, db *sql.DB) *FounderAuditService {
	return &FounderAuditService{baseService: bs, db: db}
}

// AuditAction constants for founder-visible actions (distinct from the
// experiment_metric action used by ExperimentsService.RecordMetric).
const (
	AuditActionSignalAcknowledge  = "signal_acknowledge"
	AuditActionExperimentCreate   = "experiment_create"
	AuditActionExperimentStart    = "experiment_start"
	AuditActionExperimentComplete = "experiment_complete"
	AuditActionPNLGenerate        = "pnl_generate"
	AuditActionPNLView            = "pnl_view"
	AuditActionOpsAlertResolve    = "ops_alert_resolve"
	AuditActionFounderDashboard   = "founder_dashboard_view"
)

// AuditEntry is a row in founder_audit (Spec 16 §7).
type AuditEntry struct {
	ID           string
	TenantID     string
	ActorID      string
	ActorRole    string
	Action       string
	ResourceType string
	ResourceID   string
	Details      string
	IPAddress    string
	UserAgent    string
	CreatedAt    time.Time
}

// AuditFilters scopes ListAudit queries.
type AuditFilters struct {
	Action       string
	ResourceType string
	Page         int
	Limit        int
}

// RecordAudit writes an append-only audit trail entry.
func (s *FounderAuditService) RecordAudit(ctx context.Context, entry AuditEntry) error {
	if s.db == nil {
		return fmt.Errorf("database unavailable")
	}
	if entry.TenantID == "" {
		entry.TenantID = string(shared.DefaultTenant)
	}
	id := generateDisplayID("fa")
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO founder_audit
		 (id, tenant_id, actor_id, actor_role, action, resource_type, resource_id, details, ip_address, user_agent, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		id, entry.TenantID, entry.ActorID, entry.ActorRole, entry.Action,
		entry.ResourceType, entry.ResourceID, entry.Details, entry.IPAddress, entry.UserAgent, now)
	if err != nil {
		return fmt.Errorf("insert founder audit: %w", err)
	}
	return nil
}

// ListAudit returns audit entries matching the filters, with total count.
func (s *FounderAuditService) ListAudit(ctx context.Context, tenantID string, filters AuditFilters) ([]AuditEntry, int, error) {
	if s.db == nil {
		return nil, 0, fmt.Errorf("database unavailable")
	}
	if tenantID == "" {
		tenantID = string(shared.DefaultTenant)
	}

	where := "WHERE tenant_id = ?"
	args := []interface{}{tenantID}
	if filters.Action != "" {
		where += " AND action = ?"
		args = append(args, filters.Action)
	}
	if filters.ResourceType != "" {
		where += " AND resource_type = ?"
		args = append(args, filters.ResourceType)
	}

	query := `SELECT id, tenant_id, actor_id, actor_role, action, resource_type, resource_id,
	          details, ip_address, user_agent, created_at
	          FROM founder_audit ` + where + ` ORDER BY created_at DESC`
	if filters.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d OFFSET %d", filters.Limit, filters.Page*filters.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var entries []AuditEntry
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.TenantID, &e.ActorID, &e.ActorRole, &e.Action,
			&e.ResourceType, &e.ResourceID, &e.Details, &e.IPAddress, &e.UserAgent,
			&e.CreatedAt); err != nil {
			return nil, 0, err
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	var total int
	countQuery := `SELECT COUNT(*) FROM founder_audit ` + where
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	return entries, total, nil
}
