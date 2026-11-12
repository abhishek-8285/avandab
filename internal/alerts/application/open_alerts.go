package application

// Open-alert read model behind the agent get_open_alerts tool, extracted from
// internal/agent/tools.go (handlers-end-state ADR, Phase 3). The tool keeps
// the JSON schema, argument mapping and model-facing strings; this file owns
// the tenant-scoped query and the wire shape.

import (
	"context"
	"database/sql"
	"errors"
)

// OpenAlert is one open alert row. JSON tags/field order mirror the legacy
// tool output exactly (entity_* are nullable and must not drop a row).
type OpenAlert struct {
	ID         string  `json:"id"`
	Source     string  `json:"source"`
	AlertType  string  `json:"alert_type"`
	Severity   string  `json:"severity"`
	EntityType *string `json:"entity_type"`
	EntityID   *string `json:"entity_id"`
	Title      string  `json:"title"`
	Message    string  `json:"message"`
	Status     string  `json:"status"`
	CreatedAt  string  `json:"created_at"`
}

// OpenAlertQueries is the read model facade. Stateless, cheap to construct
// per request.
type OpenAlertQueries struct {
	db *sql.DB
}

// NewOpenAlertQueries wires the read model against the database.
func NewOpenAlertQueries(db *sql.DB) *OpenAlertQueries {
	return &OpenAlertQueries{db: db}
}

// List returns up to limit open alerts for tenantID, newest first, with an
// optional severity filter. Tenant scoping is mandatory (the legacy query
// leak, audit 2026-09-17, is fixed here and pinned by tests); a nil db is an
// error so the tool can render its legacy "database not available" string.
// Rows with NULL entity_type/entity_id are kept via pointer scans.
func (q *OpenAlertQueries) List(ctx context.Context, tenantID, severity string, limit int) ([]OpenAlert, error) {
	if q == nil || q.db == nil {
		return nil, errors.New("database unavailable")
	}
	if limit <= 0 {
		limit = 20
	}
	query := `SELECT id, source, alert_type, severity, entity_type, entity_id, title, message, status, created_at
		FROM alerts WHERE status = 'open' AND tenant_id = ?`
	args := []any{tenantID}
	if severity != "" {
		query += ` AND severity = ?`
		args = append(args, severity)
	}
	query += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := q.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []OpenAlert
	for rows.Next() {
		var a OpenAlert
		if err := rows.Scan(&a.ID, &a.Source, &a.AlertType, &a.Severity, &a.EntityType, &a.EntityID, &a.Title, &a.Message, &a.Status, &a.CreatedAt); err == nil {
			out = append(out, a)
		}
	}
	return out, nil
}
