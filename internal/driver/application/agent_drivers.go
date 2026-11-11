package application

// Agent-facing driver availability read model, extracted from
// internal/agent/tools.go (handlers-end-state ADR, Phase 3).

import (
	"context"
	"database/sql"
	"strings"

	"transport-app/internal/shared"
)

// AgentDriverRow is the list_available_drivers JSON row.
type AgentDriverRow struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Phone   string `json:"phone"`
	License string `json:"license_number"`
}

// AgentDriverQueries is the driver facade. Stateless.
type AgentDriverQueries struct{ db *sql.DB }

// NewAgentDriverQueries wires the facade against the database.
func NewAgentDriverQueries(db *sql.DB) *AgentDriverQueries { return &AgentDriverQueries{db: db} }

// Available returns the tenant's available drivers (legacy status='available',
// created_at ASC ordering).
func (q *AgentDriverQueries) Available(ctx context.Context, tenantID shared.TenantID) ([]AgentDriverRow, error) {
	if q == nil || q.db == nil {
		return nil, sql.ErrNoRows
	}
	rows, err := q.db.QueryContext(ctx, `
		SELECT id, first_name, last_name, phone, license_number
		FROM drivers WHERE status = 'available' AND tenant_id = ? ORDER BY created_at ASC`, string(tenantID))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []AgentDriverRow
	for rows.Next() {
		var r AgentDriverRow
		var first, last string
		if err := rows.Scan(&r.ID, &first, &last, &r.Phone, &r.License); err != nil {
			return nil, err
		}
		r.Name = strings.TrimSpace(first + " " + last)
		out = append(out, r)
	}
	return out, rows.Err()
}
