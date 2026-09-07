package service

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"transport-app/internal/shared"
)

// FounderSignalsService implements the founder visibility layer (Spec 16 §6):
// key business metrics surfaced as "signals" that fire when thresholds are
// crossed, plus acknowledgement and listing.
type FounderSignalsService struct {
	baseService
	db    *sql.DB
	audit *FounderAuditService
}

// NewFounderSignalsService constructs a FounderSignalsService.
func NewFounderSignalsService(bs baseService, db *sql.DB) *FounderSignalsService {
	return &FounderSignalsService{baseService: bs, db: db}
}

// SetAudit wires the audit service used when signals are acknowledged.
func (s *FounderSignalsService) SetAudit(a *FounderAuditService) {
	s.audit = a
}

// Signal type constants matching CHECK constraint in 00058.
const (
	SignalRevenueMilestone       = "revenue_milestone"
	SignalCustomerChurnRisk      = "customer_churn_risk"
	SignalDriverChurnRisk        = "driver_churn_risk"
	SignalVehicleUtilization     = "vehicle_utilization"
	SignalFuelEfficiencyTrend    = "fuel_efficiency_trend"
	SignalComplianceScore        = "compliance_score"
	SignalSettlementDisputeSpike = "settlement_dispute_spike"
	SignalCashFlowAlert          = "cash_flow_alert"
)

// Direction constants matching CHECK constraint in 00058.
const (
	DirectionAbove   = "above"
	DirectionBelow   = "below"
	DirectionCrossed = "crossed"
)

// FounderSignal is a row in founder_signals (Spec 16 §6).
type FounderSignal struct {
	ID             string
	TenantID       string
	SignalType     string
	SignalValue    float64
	ThresholdValue *float64
	Direction      string
	Metadata       string
	Acknowledged   bool
	AcknowledgedBy *string
	AcknowledgedAt *time.Time
	CreatedAt      time.Time
}

// SignalFilters scopes ListSignals queries.
type SignalFilters struct {
	SignalType         string
	UnacknowledgedOnly bool
	Page               int
	Limit              int
}

// EmitSignal records a new founder signal with acknowledged=0.
func (s *FounderSignalsService) EmitSignal(ctx context.Context, signal FounderSignal) (string, error) {
	if s.db == nil {
		return "", fmt.Errorf("database unavailable")
	}
	if tid, err := shared.RequireTenantOr(ctx, signal.TenantID); err != nil {
		return "", err
	} else {
		signal.TenantID = string(tid)
	}
	id := generateDisplayID("fsig")
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO founder_signals
		 (id, tenant_id, signal_type, signal_value, threshold_value, direction, metadata, acknowledged, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, 0, $8)`,
		id, signal.TenantID, signal.SignalType, signal.SignalValue,
		signal.ThresholdValue, signal.Direction, signal.Metadata, now)
	if err != nil {
		return "", fmt.Errorf("insert founder signal: %w", err)
	}
	return id, nil
}

// EmitIfThreshold emits a signal only when value crosses the threshold, with a
// 1-hour dedup window to prevent signal spam across repeated cron runs.
func (s *FounderSignalsService) EmitIfThreshold(ctx context.Context, tenantID, signalType string, value, threshold float64, metadata string) (bool, error) {
	if s.db == nil {
		return false, fmt.Errorf("database unavailable")
	}
	tid, err := shared.RequireTenantOr(ctx, tenantID)
	if err != nil {
		return false, err
	}
	tenantID = string(tid)

	var direction string
	switch {
	case value > threshold:
		direction = DirectionAbove
	case value < threshold:
		direction = DirectionBelow
	default:
		return false, nil // exactly at threshold → no signal
	}

	// Dedup: skip if the same (type, direction) fired within the last hour.
	var existingCount int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM founder_signals
		 WHERE tenant_id = $1 AND signal_type = $2 AND direction = $3 AND created_at > $4`,
		tenantID, signalType, direction, time.Now().UTC().Add(-time.Hour)).Scan(&existingCount); err != nil {
		return false, fmt.Errorf("dedup check: %w", err)
	}
	if existingCount > 0 {
		return false, nil
	}

	thr := threshold
	id, err := s.EmitSignal(ctx, FounderSignal{
		TenantID:       tenantID,
		SignalType:     signalType,
		SignalValue:    value,
		ThresholdValue: &thr,
		Direction:      direction,
		Metadata:       metadata,
	})
	if err != nil {
		return false, err
	}
	return id != "", nil
}

// AcknowledgeSignal marks a signal acknowledged and writes an audit entry.
func (s *FounderSignalsService) AcknowledgeSignal(ctx context.Context, signalID, userID, role string) error {
	if s.db == nil {
		return fmt.Errorf("database unavailable")
	}
	if role == "" {
		role = "admin"
	}
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx,
		`UPDATE founder_signals SET acknowledged = 1, acknowledged_by = $1, acknowledged_at = $2
		 WHERE id = $3 AND acknowledged = 0`,
		userID, now, signalID)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("signal %s not found or already acknowledged", signalID)
	}

	if s.audit != nil {
		_ = s.audit.RecordAudit(ctx, AuditEntry{
			TenantID:     string(shared.TenantIDFromContext(ctx)),
			ActorID:      userID,
			ActorRole:    role,
			Action:       AuditActionSignalAcknowledge,
			ResourceType: "founder_signal",
			ResourceID:   signalID,
			Details:      "{}",
		})
	}
	return nil
}

// ListSignals returns founder signals matching the filters, with total count.
func (s *FounderSignalsService) ListSignals(ctx context.Context, tenantID string, filters SignalFilters) ([]FounderSignal, int, error) {
	if s.db == nil {
		return nil, 0, fmt.Errorf("database unavailable")
	}
	tid, err := shared.RequireTenantOr(ctx, tenantID)
	if err != nil {
		return nil, 0, err
	}
	tenantID = string(tid)

	where := "WHERE tenant_id = ?"
	args := []interface{}{tenantID}
	if filters.SignalType != "" {
		where += " AND signal_type = ?"
		args = append(args, filters.SignalType)
	}
	if filters.UnacknowledgedOnly {
		where += " AND acknowledged = 0"
	}

	query := `SELECT id, tenant_id, signal_type, signal_value, threshold_value, direction,
	          metadata, acknowledged, acknowledged_by, acknowledged_at, created_at
	          FROM founder_signals ` + where + ` ORDER BY created_at DESC`
	if filters.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d OFFSET %d", filters.Limit, filters.Page*filters.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var signals []FounderSignal
	for rows.Next() {
		sig, err := scanFounderSignal(rows)
		if err != nil {
			return nil, 0, err
		}
		signals = append(signals, sig)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	var total int
	countQuery := `SELECT COUNT(*) FROM founder_signals ` + where
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	return signals, total, nil
}

// CountUnacknowledged returns the number of unacknowledged signals for a tenant.
func (s *FounderSignalsService) CountUnacknowledged(ctx context.Context, tenantID string) (int, error) {
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
		`SELECT COUNT(*) FROM founder_signals WHERE tenant_id = $1 AND acknowledged = 0`, tenantID).Scan(&n)
	return n, err
}

// ---- Automatic signal generation hooks (Spec 16 §6) ----

// EmitRevenueMilestone fires when monthly revenue crosses a milestone threshold.
func (s *FounderSignalsService) EmitRevenueMilestone(ctx context.Context, tenantID string, monthlyRevenue, threshold float64) (bool, error) {
	return s.EmitIfThreshold(ctx, tenantID, SignalRevenueMilestone, monthlyRevenue, threshold,
		fmt.Sprintf(`{"monthly_revenue":%.2f,"threshold":%.2f}`, monthlyRevenue, threshold))
}

// EmitDriverChurnRisk fires when too many active drivers have been idle.
func (s *FounderSignalsService) EmitDriverChurnRisk(ctx context.Context, tenantID string, inactiveCount, threshold float64) (bool, error) {
	return s.EmitIfThreshold(ctx, tenantID, SignalDriverChurnRisk, inactiveCount, threshold,
		fmt.Sprintf(`{"inactive_count":%.0f,"threshold":%.0f,"window_days":14}`, inactiveCount, threshold))
}

// EmitVehicleUtilization fires when trips-per-vehicle-per-day falls below threshold.
func (s *FounderSignalsService) EmitVehicleUtilization(ctx context.Context, tenantID string, utilization, threshold float64, trips, vehicles int) (bool, error) {
	return s.EmitIfThreshold(ctx, tenantID, SignalVehicleUtilization, utilization, threshold,
		fmt.Sprintf(`{"trips":%d,"vehicles":%d,"utilization":%.2f}`, trips, vehicles, utilization))
}

// EmitSettlementDisputeSpike fires when 3+ settlement disputes occur in 7 days.
func (s *FounderSignalsService) EmitSettlementDisputeSpike(ctx context.Context, tenantID string, disputeCount, threshold float64) (bool, error) {
	return s.EmitIfThreshold(ctx, tenantID, SignalSettlementDisputeSpike, disputeCount, threshold,
		fmt.Sprintf(`{"dispute_count":%.0f,"window_days":7}`, disputeCount))
}

// EmitCashFlowAlert fires when net profit is negative.
func (s *FounderSignalsService) EmitCashFlowAlert(ctx context.Context, tenantID string, netProfit float64, date string) (bool, error) {
	return s.EmitIfThreshold(ctx, tenantID, SignalCashFlowAlert, netProfit, 0.0,
		fmt.Sprintf(`{"net_profit":%.2f,"date":"%s"}`, netProfit, date))
}

// EmitComplianceScore fires when the compliance score drops below threshold.
func (s *FounderSignalsService) EmitComplianceScore(ctx context.Context, tenantID string, score, threshold float64) (bool, error) {
	return s.EmitIfThreshold(ctx, tenantID, SignalComplianceScore, score, threshold,
		fmt.Sprintf(`{"score":%.1f,"threshold":%.1f}`, score, threshold))
}

func scanFounderSignal(rows *sql.Rows) (FounderSignal, error) {
	var sig FounderSignal
	var threshold sql.NullFloat64
	var ackBy sql.NullString
	var ackAt sql.NullTime
	if err := rows.Scan(&sig.ID, &sig.TenantID, &sig.SignalType, &sig.SignalValue,
		&threshold, &sig.Direction, &sig.Metadata, &sig.Acknowledged,
		&ackBy, &ackAt, &sig.CreatedAt); err != nil {
		return sig, err
	}
	if threshold.Valid {
		sig.ThresholdValue = &threshold.Float64
	}
	if ackBy.Valid {
		sig.AcknowledgedBy = &ackBy.String
	}
	if ackAt.Valid {
		sig.AcknowledgedAt = &ackAt.Time
	}
	return sig, nil
}
