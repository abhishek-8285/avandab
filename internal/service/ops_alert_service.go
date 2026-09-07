package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	appdb "transport-app/internal/database"

	"transport-app/internal/events"
	"transport-app/internal/shared"
)

// AlertType constants matching the CHECK constraint in 00058 (Spec 16 §4).
const (
	OpsAlertVehicleBreakdown   = "vehicle_breakdown"
	OpsAlertDriverAbsence      = "driver_absence"
	OpsAlertRouteDisruption    = "route_disruption"
	OpsAlertPaymentDelay       = "payment_delay"
	OpsAlertComplianceBreach   = "compliance_breach"
	OpsAlertFuelTheftConfirmed = "fuel_theft_confirmed"
	OpsAlertSettlementDispute  = "settlement_dispute"
	OpsAlertSystemError        = "system_error"
)

// Severity constants.
const (
	OpsAlertSeverityLow      = "low"
	OpsAlertSeverityMedium   = "medium"
	OpsAlertSeverityHigh     = "high"
	OpsAlertSeverityCritical = "critical"
)

// Status constants.
const (
	OpsAlertStatusOpen         = "open"
	OpsAlertStatusAcknowledged = "acknowledged"
	OpsAlertStatusResolved     = "resolved"
	OpsAlertStatusDismissed    = "dismissed"
)

// Sentinel errors.
var (
	ErrAlertNotFound                      = errors.New("alert not found")
	ErrAlertNotFoundOrAlreadyAcknowledged = errors.New("alert not found or already acknowledged")
	ErrAlertNotFoundOrAlreadyResolved     = errors.New("alert not found or already resolved")
	ErrAlertNotFoundOrAlreadyDismissed    = errors.New("alert not found or already dismissed")
)

// OpsAlert represents an operational alert row in ops_alerts.
type OpsAlert struct {
	ID             string     `json:"id"`
	TenantID       string     `json:"tenant_id"`
	AlertType      string     `json:"alert_type"`
	Severity       string     `json:"severity"`
	Title          string     `json:"title"`
	Description    string     `json:"description"`
	EntityType     *string    `json:"entity_type,omitempty"`
	EntityID       *string    `json:"entity_id,omitempty"`
	Status         string     `json:"status"`
	AcknowledgedBy *string    `json:"acknowledged_by,omitempty"`
	AcknowledgedAt *time.Time `json:"acknowledged_at,omitempty"`
	ResolvedBy     *string    `json:"resolved_by,omitempty"`
	ResolvedAt     *time.Time `json:"resolved_at,omitempty"`
	ResolutionNote *string    `json:"resolution_note,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

// OpsAlertFilters defines filtering criteria for ListAlerts.
type OpsAlertFilters struct {
	Status   string `json:"status"`
	Type     string `json:"type"`
	Severity string `json:"severity"`
	Page     int    `json:"page"`
	Limit    int    `json:"limit"`
}

// OpsAlertService handles operational alert creation, lifecycle, and querying (Spec 16 §4).
type OpsAlertService struct {
	baseService
	db             *sql.DB
	founderSignals *FounderSignalsService
}

// SetFounderSignals wires the founder signals service to emit dispute spikes.
func (s *OpsAlertService) SetFounderSignals(f *FounderSignalsService) {
	s.founderSignals = f
}

// NewOpsAlertService constructs an OpsAlertService with database access.
func NewOpsAlertService(bs baseService, db *sql.DB) *OpsAlertService {
	return &OpsAlertService{
		baseService: bs,
		db:          db,
	}
}

// NewOpsAlertServiceForTest creates an OpsAlertService with a custom event bus for testing.
func NewOpsAlertServiceForTest(db *sql.DB, bus events.EventBus) *OpsAlertService {
	return &OpsAlertService{
		baseService: baseService{events: bus},
		db:          db,
	}
}

// CreateAlert inserts a new operational alert with status 'open'.
// If severity is 'critical', it publishes an "ops.alert.critical" event on the event bus.
func (s *OpsAlertService) CreateAlert(ctx context.Context, alert OpsAlert) (string, error) {
	if s.db == nil {
		return "", errors.New("database unavailable")
	}

	tenantID := alert.TenantID
	if tenantID == "" {
		tenantID = string(shared.TenantIDFromContext(ctx))
	}
	if tenantID == "" {
		tenantID = string(shared.DefaultTenant)
	}

	severity := alert.Severity
	if severity == "" {
		severity = OpsAlertSeverityMedium
	}

	id := generateDisplayID("ops")
	now := time.Now().UTC()

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO ops_alerts
		 (id, tenant_id, alert_type, severity, title, description,
		  entity_type, entity_id, status, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'open', $9)`,
		id, tenantID, alert.AlertType, severity,
		alert.Title, alert.Description, alert.EntityType, alert.EntityID, now)
	if err != nil {
		return "", fmt.Errorf("insert ops alert: %w", err)
	}

	// Bridge to Spec 05 alerting pipeline if critical (Option A)
	if severity == OpsAlertSeverityCritical && s.events != nil {
		s.events.Publish(ctx, events.Event{
			Type: "ops.alert.critical",
			Payload: map[string]interface{}{
				"alert_id":    id,
				"alert_type":  alert.AlertType,
				"severity":    severity,
				"title":       alert.Title,
				"description": alert.Description,
				"entity_type": alert.EntityType,
				"entity_id":   alert.EntityID,
				"tenant_id":   tenantID,
			},
		})
	}

	// Founder signal: spike in settlement disputes (Spec 16 §6).
	if alert.AlertType == OpsAlertSettlementDispute && s.founderSignals != nil {
		count, _ := s.CountAlertsSince(ctx, tenantID, OpsAlertSettlementDispute, 7*24*time.Hour)
		if count >= 3 {
			_, _ = s.founderSignals.EmitSettlementDisputeSpike(ctx, tenantID, float64(count), 3.0)
		}
	}

	return id, nil
}

// CountAlertsSince returns the number of alerts of a given type created within
// the last `since` duration for a tenant. Used for founder-signal spike detection.
func (s *OpsAlertService) CountAlertsSince(ctx context.Context, tenantID, alertType string, since time.Duration) (int, error) {
	if s.db == nil {
		return 0, fmt.Errorf("database unavailable")
	}
	if tenantID == "" {
		tenantID = string(shared.DefaultTenant)
	}
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM ops_alerts
		 WHERE tenant_id = $1 AND alert_type = $2 AND created_at > $3`,
		tenantID, alertType, time.Now().UTC().Add(-since)).Scan(&n)
	if err != nil {
		return 0, err
	}
	return n, nil
}

// CountByStatus returns the number of alerts in a given status for a tenant.
func (s *OpsAlertService) CountByStatus(ctx context.Context, tenantID, status string) (int, error) {
	if s.db == nil {
		return 0, fmt.Errorf("database unavailable")
	}
	if tenantID == "" {
		tenantID = string(shared.DefaultTenant)
	}
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM ops_alerts WHERE tenant_id = $1 AND status = $2`, tenantID, status).Scan(&n)
	if err != nil {
		return 0, err
	}
	return n, nil
}

// AcknowledgeAlert transitions an alert from 'open' to 'acknowledged'.
func (s *OpsAlertService) AcknowledgeAlert(ctx context.Context, alertID, userID string) error {
	if s.db == nil {
		return errors.New("database unavailable")
	}
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx,
		`UPDATE ops_alerts
		 SET status = 'acknowledged', acknowledged_by = $1, acknowledged_at = $2
		 WHERE id = $3 AND status = 'open'`,
		userID, now, alertID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrAlertNotFoundOrAlreadyAcknowledged
	}
	return nil
}

// ResolveAlert transitions an alert from 'open' or 'acknowledged' to 'resolved'.
func (s *OpsAlertService) ResolveAlert(ctx context.Context, alertID, userID, note string) error {
	if s.db == nil {
		return errors.New("database unavailable")
	}
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx,
		`UPDATE ops_alerts
		 SET status = 'resolved', resolved_by = $1, resolved_at = $2, resolution_note = $3
		 WHERE id = $4 AND status IN ('open', 'acknowledged')`,
		userID, now, note, alertID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrAlertNotFoundOrAlreadyResolved
	}
	return nil
}

// DismissAlert transitions an alert from 'open' or 'acknowledged' to 'dismissed'.
func (s *OpsAlertService) DismissAlert(ctx context.Context, alertID, userID, reason string) error {
	if s.db == nil {
		return errors.New("database unavailable")
	}
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx,
		`UPDATE ops_alerts
		 SET status = 'dismissed', resolved_by = $1, resolved_at = $2, resolution_note = $3
		 WHERE id = $4 AND status IN ('open', 'acknowledged')`,
		userID, now, reason, alertID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrAlertNotFoundOrAlreadyDismissed
	}
	return nil
}

// GetAlert retrieves a single operational alert by ID.
func (s *OpsAlertService) GetAlert(ctx context.Context, alertID string) (*OpsAlert, error) {
	if s.db == nil {
		return nil, errors.New("database unavailable")
	}

	var alert OpsAlert
	var entType, entID, ackBy, resBy, resNote sql.NullString
	var ackAt, resAt sql.NullTime

	err := s.db.QueryRowContext(ctx,
		`SELECT id, tenant_id, alert_type, severity, title, COALESCE(description, ''),
		        entity_type, entity_id, status, acknowledged_by, acknowledged_at,
		        resolved_by, resolved_at, resolution_note, created_at
		 FROM ops_alerts
		 WHERE id = $1`, alertID).Scan(
		&alert.ID, &alert.TenantID, &alert.AlertType, &alert.Severity, &alert.Title, &alert.Description,
		&entType, &entID, &alert.Status, &ackBy, &ackAt, &resBy, &resAt, &resNote, &alert.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrAlertNotFound
	}
	if err != nil {
		return nil, err
	}

	if entType.Valid {
		alert.EntityType = &entType.String
	}
	if entID.Valid {
		alert.EntityID = &entID.String
	}
	if ackBy.Valid {
		alert.AcknowledgedBy = &ackBy.String
	}
	if ackAt.Valid {
		alert.AcknowledgedAt = &ackAt.Time
	}
	if resBy.Valid {
		alert.ResolvedBy = &resBy.String
	}
	if resAt.Valid {
		alert.ResolvedAt = &resAt.Time
	}
	if resNote.Valid {
		alert.ResolutionNote = &resNote.String
	}

	return &alert, nil
}

// ListAlerts returns a paginated list of operational alerts and total count matching filters.
func (s *OpsAlertService) ListAlerts(ctx context.Context, tenantID string, filters OpsAlertFilters) ([]OpsAlert, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("database unavailable")
	}

	if tenantID == "" {
		tenantID = string(shared.TenantIDFromContext(ctx))
	}
	if tenantID == "" {
		tenantID = string(shared.DefaultTenant)
	}

	var whereClauses []string
	var args []interface{}

	whereClauses = append(whereClauses, "tenant_id = ?")
	args = append(args, tenantID)

	if filters.Status != "" {
		whereClauses = append(whereClauses, "status = ?")
		args = append(args, filters.Status)
	}
	if filters.Type != "" {
		whereClauses = append(whereClauses, "alert_type = ?")
		args = append(args, filters.Type)
	}
	if filters.Severity != "" {
		whereClauses = append(whereClauses, "severity = ?")
		args = append(args, filters.Severity)
	}

	whereSQL := strings.Join(whereClauses, " AND ")

	// 1. Get total count
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM ops_alerts WHERE %s", whereSQL)
	reboundCount, rerr := appdb.Rebind(countQuery)
	if rerr != nil {
		return nil, 0, fmt.Errorf("count ops alerts: %w", rerr)
	}
	var total int
	if err := s.db.QueryRowContext(ctx, reboundCount, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count ops alerts: %w", err)
	}

	// 2. Query page
	limit := filters.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	page := filters.Page
	if page <= 0 {
		page = 1
	}
	offset := (page - 1) * limit

	query := fmt.Sprintf(`
		SELECT id, tenant_id, alert_type, severity, title, COALESCE(description, ''),
		       entity_type, entity_id, status, acknowledged_by, acknowledged_at,
		       resolved_by, resolved_at, resolution_note, created_at
		FROM ops_alerts
		WHERE %s
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?`, whereSQL)

	queryArgs := make([]interface{}, len(args), len(args)+2)
	copy(queryArgs, args)
	queryArgs = append(queryArgs, limit, offset)
	rebound, rerr := appdb.Rebind(query)
	if rerr != nil {
		return nil, 0, fmt.Errorf("query ops alerts: %w", rerr)
	}
	rows, err := s.db.QueryContext(ctx, rebound, queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("query ops alerts: %w", err)
	}
	defer rows.Close()

	var alerts []OpsAlert
	for rows.Next() {
		var a OpsAlert
		var entType, entID, ackBy, resBy, resNote sql.NullString
		var ackAt, resAt sql.NullTime

		if err := rows.Scan(
			&a.ID, &a.TenantID, &a.AlertType, &a.Severity, &a.Title, &a.Description,
			&entType, &entID, &a.Status, &ackBy, &ackAt, &resBy, &resAt, &resNote, &a.CreatedAt,
		); err != nil {
			return nil, 0, err
		}

		if entType.Valid {
			a.EntityType = &entType.String
		}
		if entID.Valid {
			a.EntityID = &entID.String
		}
		if ackBy.Valid {
			a.AcknowledgedBy = &ackBy.String
		}
		if ackAt.Valid {
			a.AcknowledgedAt = &ackAt.Time
		}
		if resBy.Valid {
			a.ResolvedBy = &resBy.String
		}
		if resAt.Valid {
			a.ResolvedAt = &resAt.Time
		}
		if resNote.Valid {
			a.ResolutionNote = &resNote.String
		}

		alerts = append(alerts, a)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	if alerts == nil {
		alerts = []OpsAlert{}
	}

	return alerts, total, nil
}
