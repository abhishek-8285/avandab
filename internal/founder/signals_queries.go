package founder

// Read-only founder-signal queries for the founder dashboard handlers,
// extracted from the legacy service container wiring (handlers-end-state ADR,
// Phase 4 increment 3). Write paths (emit/acknowledge) stay in the legacy
// service until the founder write handlers migrate; this facade covers the
// two dashboard reads only.

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"transport-app/internal/shared"
)

// Signal is one founder_signals row (legacy-compatible field names).
type Signal struct {
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

// SignalFilters scopes ListSignals.
type SignalFilters struct {
	SignalType         string
	UnacknowledgedOnly bool
	Page               int
	Limit              int
}

// SignalQueries is the read facade. Stateless.
type SignalQueries struct{ db *sql.DB }

// NewSignalQueries wires the facade against the database.
func NewSignalQueries(db *sql.DB) *SignalQueries { return &SignalQueries{db: db} }

// CountUnacknowledged returns the tenant's unacknowledged signal count.
func (q *SignalQueries) CountUnacknowledged(ctx context.Context, tenantID string) (int, error) {
	if q == nil || q.db == nil {
		return 0, sql.ErrNoRows
	}
	tid, err := shared.RequireTenantOr(ctx, tenantID)
	if err != nil {
		return 0, err
	}
	var n int
	err = q.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM founder_signals WHERE tenant_id = $1 AND acknowledged = 0`, string(tid)).Scan(&n)
	return n, err
}

// ListSignals returns matching signals, newest first, with the total count.
func (q *SignalQueries) ListSignals(ctx context.Context, tenantID string, filters SignalFilters) ([]Signal, int, error) {
	if q == nil || q.db == nil {
		return nil, 0, sql.ErrNoRows
	}
	tid, err := shared.RequireTenantOr(ctx, tenantID)
	if err != nil {
		return nil, 0, err
	}
	where := "WHERE tenant_id = ?"
	args := []any{string(tid)}
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
	rows, err := q.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	var out []Signal
	for rows.Next() {
		sig, err := scanSignal(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, sig)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	var total int
	if err := q.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM founder_signals `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// FindByID returns a signal by ID within the tenant, or nil if not found.
func (q *SignalQueries) FindByID(ctx context.Context, tenantID, id string) (*Signal, error) {
	if q == nil || q.db == nil {
		return nil, sql.ErrNoRows
	}
	tid, err := shared.RequireTenantOr(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	query := `SELECT id, tenant_id, signal_type, signal_value, threshold_value, direction,
	          metadata, acknowledged, acknowledged_by, acknowledged_at, created_at
	          FROM founder_signals WHERE tenant_id = $1 AND id = $2`
	row := q.db.QueryRowContext(ctx, query, string(tid), id)
	var s Signal
	var threshold sql.NullFloat64
	var ackBy sql.NullString
	var ackAt sql.NullTime
	if err := row.Scan(&s.ID, &s.TenantID, &s.SignalType, &s.SignalValue,
		&threshold, &s.Direction, &s.Metadata, &s.Acknowledged, &ackBy, &ackAt, &s.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if threshold.Valid {
		s.ThresholdValue = &threshold.Float64
	}
	if ackBy.Valid {
		s.AcknowledgedBy = &ackBy.String
	}
	if ackAt.Valid {
		s.AcknowledgedAt = &ackAt.Time
	}
	return &s, nil
}

// Acknowledge updates a founder_signals row as acknowledged.
func (q *SignalQueries) Acknowledge(ctx context.Context, tenantID, signalID, userID string) error {
	if q == nil || q.db == nil {
		return sql.ErrNoRows
	}
	tid, err := shared.RequireTenantOr(ctx, tenantID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	result, err := q.db.ExecContext(ctx,
		`UPDATE founder_signals SET acknowledged = 1, acknowledged_by = $1, acknowledged_at = $2
		 WHERE id = $3 AND tenant_id = $4 AND acknowledged = 0`,
		userID, now, signalID, string(tid))
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("signal %s not found or already acknowledged", signalID)
	}
	return nil
}

func scanSignal(rows *sql.Rows) (Signal, error) {
	var s Signal
	var threshold sql.NullFloat64
	var ackBy sql.NullString
	var ackAt sql.NullTime
	if err := rows.Scan(&s.ID, &s.TenantID, &s.SignalType, &s.SignalValue,
		&threshold, &s.Direction, &s.Metadata, &s.Acknowledged, &ackBy, &ackAt, &s.CreatedAt); err != nil {
		return s, err
	}
	if threshold.Valid {
		s.ThresholdValue = &threshold.Float64
	}
	if ackBy.Valid {
		s.AcknowledgedBy = &ackBy.String
	}
	if ackAt.Valid {
		s.AcknowledgedAt = &ackAt.Time
	}
	return s, nil
}
