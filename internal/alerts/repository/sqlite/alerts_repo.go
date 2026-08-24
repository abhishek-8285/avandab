package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"transport-app/internal/alerts/domain"
	"transport-app/internal/alerts/repository"
	"transport-app/internal/shared"
)

type sqlAlertRepository struct {
	db *sql.DB
}

// NewAlertRepository creates a new SQLite-backed AlertRepository.
func NewAlertRepository(db *sql.DB) repository.AlertRepository {
	return &sqlAlertRepository{db: db}
}

// alertColumns is the canonical SELECT list for alert rows; order matches
// scanAlert exactly.
const alertColumns = `
	id, rule_id, source, alert_type, severity, status, dedup_key,
	tenant_id, ack_status, severity_rank, money_at_risk, snoozed_until,
	entity_type, entity_id, user_id, title, message, occurrences,
	first_seen_at, last_seen_at, next_escalation_at, escalation_step,
	latitude, longitude, metadata, acked_by, acked_at, resolved_by, resolved_at,
	created_at, updated_at`

// scanAlert scans one alert row (alertColumns order) into domain.Alert.
func scanAlert(scan func(dest ...any) error) (domain.Alert, error) {
	var a domain.Alert
	var ruleID, entityType, entityID, userID, metadata sql.NullString
	var ackedBy, resolvedBy sql.NullString
	var nextEscalationAt, ackedAt, resolvedAt, snoozedUntil sql.NullTime
	var lat, lng sql.NullFloat64
	var moneyAtRisk float64

	err := scan(
		&a.ID, &ruleID, &a.Source, &a.AlertType, &a.Severity, &a.Status, &a.DedupKey,
		&a.TenantID, &a.AckStatus, &a.SeverityRank, &moneyAtRisk, &snoozedUntil,
		&entityType, &entityID, &userID, &a.Title, &a.Message, &a.Occurrences,
		&a.FirstSeenAt, &a.LastSeenAt, &nextEscalationAt, &a.EscalationStep,
		&lat, &lng, &metadata, &ackedBy, &ackedAt, &resolvedBy, &resolvedAt,
		&a.CreatedAt, &a.UpdatedAt,
	)
	if err != nil {
		return a, err
	}

	a.MoneyAtRisk = moneyAtRisk
	if ruleID.Valid {
		a.RuleID = &ruleID.String
	}
	if entityType.Valid {
		a.EntityType = &entityType.String
	}
	if entityID.Valid {
		a.EntityID = &entityID.String
	}
	if userID.Valid {
		a.UserID = &userID.String
	}
	if metadata.Valid {
		a.Metadata = metadata.String
	}
	if ackedBy.Valid {
		a.AckedBy = &ackedBy.String
	}
	if resolvedBy.Valid {
		a.ResolvedBy = &resolvedBy.String
	}
	if nextEscalationAt.Valid {
		a.NextEscalationAt = &nextEscalationAt.Time
	}
	if ackedAt.Valid {
		a.AckedAt = &ackedAt.Time
	}
	if resolvedAt.Valid {
		a.ResolvedAt = &resolvedAt.Time
	}
	if snoozedUntil.Valid {
		a.SnoozedUntil = &snoozedUntil.Time
	}
	if lat.Valid {
		a.Latitude = &lat.Float64
	}
	if lng.Valid {
		a.Longitude = &lng.Float64
	}
	return a, nil
}

func (r *sqlAlertRepository) FindOpenByDedupKey(ctx context.Context, dedupKey string) (*domain.Alert, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT `+alertColumns+`
		FROM alerts
		WHERE dedup_key = ? AND status IN ('open', 'acknowledged', 'escalated')
		ORDER BY last_seen_at DESC
		LIMIT 1`, dedupKey)

	a, err := scanAlert(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &a, nil
}

func (r *sqlAlertRepository) CreateAlert(ctx context.Context, a *domain.Alert) error {
	now := time.Now().UTC()
	if a.FirstSeenAt.IsZero() {
		a.FirstSeenAt = now
	}
	if a.LastSeenAt.IsZero() {
		a.LastSeenAt = now
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = now
	}
	if a.UpdatedAt.IsZero() {
		a.UpdatedAt = now
	}
	if a.Occurrences == 0 {
		a.Occurrences = 1
	}
	if a.Status == "" {
		a.Status = domain.StatusOpen
	}
	if a.AckStatus == "" {
		// Derive inbox lifecycle from legacy status so direct inserts
		// satisfy the 00092 CHECK constraint.
		switch a.Status {
		case domain.StatusAcknowledged:
			a.AckStatus = domain.AckStatusAcked
		case domain.StatusResolved, domain.StatusClosed:
			a.AckStatus = domain.AckStatusResolved
		default:
			a.AckStatus = domain.AckStatusOpen
		}
	}
	if a.TenantID == "" {
		a.TenantID = string(shared.DefaultTenant)
	}
	if a.SeverityRank == 0 {
		a.SeverityRank = domain.SeverityToRank(a.Severity)
	}

	_, err := r.db.ExecContext(ctx, `
		INSERT INTO alerts (
			id, rule_id, source, alert_type, severity, status, dedup_key,
			tenant_id, ack_status, severity_rank, money_at_risk, snoozed_until,
			entity_type, entity_id, user_id, title, message, occurrences,
			first_seen_at, last_seen_at, next_escalation_at, escalation_step,
			latitude, longitude, metadata, acked_by, acked_at, resolved_by, resolved_at,
			created_at, updated_at
		) VALUES (
			?, ?, ?, ?, ?, ?, ?,
			?, ?, ?, ?, ?,
			?, ?, ?, ?, ?, ?,
			?, ?, ?, ?,
			?, ?, ?, ?, ?, ?, ?,
			?, ?
		)`,
		a.ID, a.RuleID, a.Source, a.AlertType, a.Severity, a.Status, a.DedupKey,
		a.TenantID, a.AckStatus, a.SeverityRank, a.MoneyAtRisk, a.SnoozedUntil,
		a.EntityType, a.EntityID, a.UserID, a.Title, a.Message, a.Occurrences,
		a.FirstSeenAt, a.LastSeenAt, a.NextEscalationAt, a.EscalationStep,
		a.Latitude, a.Longitude, a.Metadata, a.AckedBy, a.AckedAt, a.ResolvedBy, a.ResolvedAt,
		a.CreatedAt, a.UpdatedAt,
	)
	return err
}

func (r *sqlAlertRepository) IncrementOccurrences(ctx context.Context, alertID string, lastSeen time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE alerts
		SET occurrences = occurrences + 1,
		    last_seen_at = ?,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`, lastSeen, alertID)
	return err
}

func (r *sqlAlertRepository) ListRulesBySource(ctx context.Context, source string) ([]domain.Rule, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, source, alert_type, name, severity, threshold, threshold_unit,
		       dedup_key_expr, cooldown_seconds, storm_window_seconds, storm_batch_min,
		       channel_routing, escalation_schedule, is_active, created_at, updated_at
		FROM alert_rules
		WHERE source = ? AND is_active = 1`, source)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []domain.Rule
	for rows.Next() {
		var rule domain.Rule
		var threshold sql.NullFloat64
		var thresholdUnit, escalationSchedule sql.NullString
		var isActive int

		err := rows.Scan(
			&rule.ID, &rule.Source, &rule.AlertType, &rule.Name, &rule.Severity,
			&threshold, &thresholdUnit, &rule.DedupKeyExpr, &rule.CooldownSeconds,
			&rule.StormWindowSeconds, &rule.StormBatchMin, &rule.ChannelRouting,
			&escalationSchedule, &isActive, &rule.CreatedAt, &rule.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		if threshold.Valid {
			rule.Threshold = &threshold.Float64
		}
		if thresholdUnit.Valid {
			rule.ThresholdUnit = &thresholdUnit.String
		}
		if escalationSchedule.Valid {
			rule.EscalationSchedule = &escalationSchedule.String
		}
		rule.IsActive = isActive == 1
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

func (r *sqlAlertRepository) GetRule(ctx context.Context, ruleID string) (*domain.Rule, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, source, alert_type, name, severity, threshold, threshold_unit,
		       dedup_key_expr, cooldown_seconds, storm_window_seconds, storm_batch_min,
		       channel_routing, escalation_schedule, is_active, created_at, updated_at
		FROM alert_rules
		WHERE id = ?`, ruleID)

	var rule domain.Rule
	var threshold sql.NullFloat64
	var thresholdUnit, escalationSchedule sql.NullString
	var isActive int

	err := row.Scan(
		&rule.ID, &rule.Source, &rule.AlertType, &rule.Name, &rule.Severity,
		&threshold, &thresholdUnit, &rule.DedupKeyExpr, &rule.CooldownSeconds,
		&rule.StormWindowSeconds, &rule.StormBatchMin, &rule.ChannelRouting,
		&escalationSchedule, &isActive, &rule.CreatedAt, &rule.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if threshold.Valid {
		rule.Threshold = &threshold.Float64
	}
	if thresholdUnit.Valid {
		rule.ThresholdUnit = &thresholdUnit.String
	}
	if escalationSchedule.Valid {
		rule.EscalationSchedule = &escalationSchedule.String
	}
	rule.IsActive = isActive == 1
	return &rule, nil
}

func (r *sqlAlertRepository) GetActiveRuleForType(ctx context.Context, source, alertType string, entityID string) (*domain.Rule, *domain.RuleOverride, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, source, alert_type, name, severity, threshold, threshold_unit,
		       dedup_key_expr, cooldown_seconds, storm_window_seconds, storm_batch_min,
		       channel_routing, escalation_schedule, is_active, created_at, updated_at
		FROM alert_rules
		WHERE source = ? AND alert_type = ? AND is_active = 1
		LIMIT 1`, source, alertType)

	var rule domain.Rule
	var threshold sql.NullFloat64
	var thresholdUnit, escalationSchedule sql.NullString
	var isActive int

	err := row.Scan(
		&rule.ID, &rule.Source, &rule.AlertType, &rule.Name, &rule.Severity,
		&threshold, &thresholdUnit, &rule.DedupKeyExpr, &rule.CooldownSeconds,
		&rule.StormWindowSeconds, &rule.StormBatchMin, &rule.ChannelRouting,
		&escalationSchedule, &isActive, &rule.CreatedAt, &rule.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	if threshold.Valid {
		rule.Threshold = &threshold.Float64
	}
	if thresholdUnit.Valid {
		rule.ThresholdUnit = &thresholdUnit.String
	}
	if escalationSchedule.Valid {
		rule.EscalationSchedule = &escalationSchedule.String
	}
	rule.IsActive = isActive == 1

	if entityID != "" {
		override, _ := r.GetOverrides(ctx, rule.ID, entityID)
		return &rule, override, nil
	}

	return &rule, nil, nil
}

func (r *sqlAlertRepository) GetOverrides(ctx context.Context, ruleID string, entityID string) (*domain.RuleOverride, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, rule_id, entity_id, severity, threshold, cooldown_seconds, channels, is_active, created_at
		FROM rule_overrides
		WHERE rule_id = ? AND (entity_id = ? OR entity_id IS NULL) AND is_active = 1
		ORDER BY entity_id DESC
		LIMIT 1`, ruleID, entityID)

	var o domain.RuleOverride
	var entityIDVal, severity, channels sql.NullString
	var threshold sql.NullFloat64
	var cooldown sql.NullInt64
	var isActive int

	err := row.Scan(&o.ID, &o.RuleID, &entityIDVal, &severity, &threshold, &cooldown, &channels, &isActive, &o.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if entityIDVal.Valid {
		o.EntityID = &entityIDVal.String
	}
	if severity.Valid {
		o.Severity = &severity.String
	}
	if threshold.Valid {
		o.Threshold = &threshold.Float64
	}
	if cooldown.Valid {
		c := int(cooldown.Int64)
		o.CooldownSeconds = &c
	}
	if channels.Valid {
		o.Channels = &channels.String
	}
	o.IsActive = isActive == 1
	return &o, nil
}

func (r *sqlAlertRepository) ListAlerts(ctx context.Context, status string, limit, offset int) ([]domain.Alert, error) {
	query := `
		SELECT ` + alertColumns + `
		FROM alerts `
	var args []interface{}
	if status != "" {
		query += "WHERE status = ? "
		args = append(args, status)
	}
	query += "ORDER BY last_seen_at DESC "
	if limit > 0 {
		query += "LIMIT ? OFFSET ? "
		args = append(args, limit, offset)
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return collectAlerts(rows)
}

func (r *sqlAlertRepository) UnreadCount(ctx context.Context, userID string) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM alerts
		WHERE status IN ('open', 'acknowledged', 'escalated')
		  AND (user_id = ? OR user_id IS NULL)`, userID).Scan(&count)
	return count, err
}

func (r *sqlAlertRepository) Recent(ctx context.Context, userID string, limit int) ([]domain.Alert, error) {
	if limit <= 0 {
		limit = 5
	}
	return r.ListAlerts(ctx, "", limit, 0)
}

func (r *sqlAlertRepository) Ack(ctx context.Context, alertID string, userID string) error {
	now := time.Now().UTC()
	_, err := r.db.ExecContext(ctx, `
		UPDATE alerts
		SET status = 'acknowledged',
		    next_escalation_at = NULL,
		    acked_by = ?,
		    acked_at = ?,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status IN ('open', 'escalated')`, userID, now, alertID)
	return err
}

func (r *sqlAlertRepository) Resolve(ctx context.Context, alertID string, userID string) error {
	now := time.Now().UTC()
	_, err := r.db.ExecContext(ctx, `
		UPDATE alerts
		SET status = 'resolved',
		    next_escalation_at = NULL,
		    resolved_by = ?,
		    resolved_at = ?,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status IN ('open', 'acknowledged', 'escalated')`, userID, now, alertID)
	return err
}

func (r *sqlAlertRepository) MarkAllRead(ctx context.Context, userID string) error {
	now := time.Now().UTC()
	_, err := r.db.ExecContext(ctx, `
		UPDATE alerts
		SET status = 'acknowledged',
		    acked_by = ?,
		    acked_at = ?,
		    updated_at = CURRENT_TIMESTAMP
		WHERE status = 'open' AND (user_id = ? OR user_id IS NULL)`, userID, now, userID)
	return err
}

func (r *sqlAlertRepository) ListPendingEscalations(ctx context.Context, now time.Time) ([]domain.Alert, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+alertColumns+`
		FROM alerts
		WHERE status IN ('open', 'escalated')
		  AND next_escalation_at IS NOT NULL
		  AND next_escalation_at <= ?
		ORDER BY next_escalation_at ASC`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return collectAlerts(rows)
}

func (r *sqlAlertRepository) UpdateEscalation(ctx context.Context, alertID string, nextStep int, nextAt *time.Time, status string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE alerts
		SET escalation_step = ?,
		    next_escalation_at = ?,
		    status = ?,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`, nextStep, nextAt, status, alertID)
	return err
}

func (r *sqlAlertRepository) ListUnflushedStormAlerts(ctx context.Context, windowCutoff time.Time) ([]domain.Alert, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+alertColumns+`
		FROM alerts
		WHERE status IN ('open', 'escalated')
		  AND occurrences > 1
		  AND last_seen_at <= ?
		  AND (metadata NOT LIKE '%"flushed":true%' OR metadata IS NULL)
		ORDER BY last_seen_at ASC`, windowCutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return collectAlerts(rows)
}

func (r *sqlAlertRepository) UpdateMetadata(ctx context.Context, alertID string, metadata string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE alerts
		SET metadata = ?,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`, metadata, alertID)
	return err
}

// collectAlerts drains a query result using scanAlert.
func collectAlerts(rows *sql.Rows) ([]domain.Alert, error) {
	defer func() { _ = rows.Close() }()
	var alerts []domain.Alert
	for rows.Next() {
		a, err := scanAlert(rows.Scan)
		if err != nil {
			return nil, err
		}
		alerts = append(alerts, a)
	}
	return alerts, rows.Err()
}

// ListInbox returns ranked alerts for one tenant (Spec 22 §2.1).
// status: open|snoozed|acked|resolved|all. Snoozed rows past their
// snoozed_until count as open (visible again). Ordered severity_rank ASC,
// created_at DESC per Spec 22 §5.1.
func (r *sqlAlertRepository) ListInbox(ctx context.Context, tenantID, status string, limit int) ([]domain.Alert, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	now := time.Now().UTC()
	query := `
		SELECT ` + alertColumns + `
		FROM alerts
		WHERE tenant_id = ?`
	var args []any
	args = append(args, tenantID)
	switch status {
	case "open":
		query += ` AND (ack_status = 'open' OR (ack_status = 'snoozed' AND snoozed_until <= ?))`
		args = append(args, now)
	case "snoozed", "acked", "resolved":
		query += ` AND ack_status = ?`
		args = append(args, status)
	default:
		// "all" or unknown — no ack_status filter.
	}
	query += ` ORDER BY severity_rank ASC, created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return collectAlerts(rows)
}

// InboxCounts implements repository.AlertRepository.
func (r *sqlAlertRepository) InboxCounts(ctx context.Context, tenantID string) (int, int, error) {
	now := time.Now().UTC()
	var open, critical int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*),
		       COALESCE(SUM(CASE WHEN severity_rank = 1 THEN 1 ELSE 0 END), 0)
		FROM alerts
		WHERE tenant_id = ?
		  AND (ack_status = 'open' OR (ack_status = 'snoozed' AND snoozed_until <= ?))`,
		tenantID, now).Scan(&open, &critical)
	if err != nil {
		return 0, 0, err
	}
	return open, critical, nil
}

// InboxAck acknowledges from the inbox. Returns false when the row was not
// in ack_status='open' (Spec 22 edge case 10: second admin's ack is a
// harmless no-op).
func (r *sqlAlertRepository) InboxAck(ctx context.Context, alertID, userID string) (bool, error) {
	now := time.Now().UTC()
	res, err := r.db.ExecContext(ctx, `
		UPDATE alerts
		SET ack_status = 'acked',
		    status = 'acknowledged',
		    next_escalation_at = NULL,
		    acked_by = ?,
		    acked_at = ?,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND ack_status = 'open'`, userID, now, alertID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// InboxSnooze hides an open alert until until. Only open rows can be
// snoozed; re-fires while snoozed increment the same row via dedup without
// lifting the snooze (Spec 22 edge case 7).
func (r *sqlAlertRepository) InboxSnooze(ctx context.Context, alertID, userID string, until time.Time) (bool, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE alerts
		SET ack_status = 'snoozed',
		    snoozed_until = ?,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND ack_status = 'open'`, until, alertID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// InboxSnoozeAll batch-snoozes open alerts by id; returns how many rows it
// actually moved (already-handled ids are skipped silently).
func (r *sqlAlertRepository) InboxSnoozeAll(ctx context.Context, ids []string, userID string, until time.Time) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, 0, len(ids)+2)
	args = append(args, until)
	for _, id := range ids {
		args = append(args, id)
	}
	res, err := r.db.ExecContext(ctx, `
		UPDATE alerts
		SET ack_status = 'snoozed',
		    snoozed_until = ?,
		    updated_at = CURRENT_TIMESTAMP
		WHERE ack_status = 'open' AND id IN (`+placeholders+`)`, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ReopenExpiredSnoozes flips expired snoozes back to open so they surface
// in the inbox again (single sweep worker, Spec 22 §5.1).
func (r *sqlAlertRepository) ReopenExpiredSnoozes(ctx context.Context, now time.Time) (int64, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE alerts
		SET ack_status = 'open',
		    snoozed_until = NULL,
		    updated_at = CURRENT_TIMESTAMP
		WHERE ack_status = 'snoozed' AND snoozed_until IS NOT NULL AND snoozed_until <= ?`, now)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
