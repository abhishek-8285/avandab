package application

// Agent-facing vehicle availability read model, extracted from
// internal/agent/tools.go (handlers-end-state ADR, Phase 3). The maintenance
// block filter mirrors the legacy IsMaintenanceBlocked rule (due date,
// unresolved critical DTC or in-process work order, unless an active override
// lifts it).

import (
	"context"
	"database/sql"

	"transport-app/internal/shared"
)

// AgentVehicleRow is the list_available_vehicles JSON row.
type AgentVehicleRow struct {
	ID       string `json:"id"`
	Number   string `json:"registration_number"`
	Type     string `json:"type"`
	Capacity int64  `json:"capacity"`
}

// AgentVehicleQueries is the vehicle facade. Stateless.
type AgentVehicleQueries struct{ db *sql.DB }

// NewAgentVehicleQueries wires the facade against the database.
func NewAgentVehicleQueries(db *sql.DB) *AgentVehicleQueries { return &AgentVehicleQueries{db: db} }

// Available returns the tenant's available vehicles, dropping those blocked
// for maintenance (legacy tool behavior).
func (q *AgentVehicleQueries) Available(ctx context.Context, tenantID shared.TenantID) ([]AgentVehicleRow, error) {
	if q == nil || q.db == nil {
		return nil, sql.ErrNoRows
	}
	rows, err := q.db.QueryContext(ctx, `
		SELECT id, registration_number, vehicle_type, capacity, maintenance_due,
		       COALESCE(maintenance_override_by,''), maintenance_override_at
		FROM vehicles WHERE status = 'available' AND tenant_id = ? ORDER BY created_at ASC`, string(tenantID))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []AgentVehicleRow
	for rows.Next() {
		var r AgentVehicleRow
		var due sql.NullString
		var overrideBy string
		var overrideAt sql.NullTime
		if err := rows.Scan(&r.ID, &r.Number, &r.Type, &r.Capacity, &due, &overrideBy, &overrideAt); err != nil {
			return nil, err
		}
		if q.maintenanceBlocked(ctx, r.ID, due, overrideBy, overrideAt) {
			continue
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// maintenanceBlocked mirrors repository/sqlite.IsMaintenanceBlocked's decision
// (any of: due date set, unresolved critical DTC, in-process work order) unless
// an active override is present. Best-effort like the legacy tool: lookup
// errors do not block the vehicle.
func (q *AgentVehicleQueries) maintenanceBlocked(ctx context.Context, vehicleID string, due sql.NullString, overrideBy string, overrideAt sql.NullTime) bool {
	if due.Valid && due.String != "" && overrideBy != "" && overrideAt.Valid {
		return false
	}
	var dtcCode string
	_ = q.db.QueryRowContext(ctx, `SELECT dtc_code FROM dtc_events WHERE vehicle_id = $1 AND severity = 'critical' AND resolved_at IS NULL ORDER BY occurred_at DESC LIMIT 1`, vehicleID).Scan(&dtcCode)
	var activeWO string
	_ = q.db.QueryRowContext(ctx, `SELECT id FROM work_orders WHERE vehicle_id = $1 AND status IN ('in_progress', 'assigned') LIMIT 1`, vehicleID).Scan(&activeWO)
	if !(due.Valid && due.String != "") && dtcCode == "" && activeWO == "" {
		return false
	}
	if overrideBy != "" && overrideAt.Valid {
		return false
	}
	return true
}
