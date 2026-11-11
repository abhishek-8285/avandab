package application

// Compliance dashboard aggregation, extracted from
// internal/handlers/compliance.go (handlers-end-state ADR, Phase 2). The
// handler keeps HTTP/JSON/HTML rendering; this file keeps the four
// dashboard read queries and their expiry/blocked-document rules.
//
// Tenant isolation preserved exactly: org callers see only their own fleet;
// the bootstrap tenant ('1'/empty: single-tenant deploys + platform views)
// keeps the legacy global aggregation. Expired documents block an entity and
// win the reason string; documents expiring within 7 days are counted as
// expiring-soon. Pending-document counts are tenant-scoped through the owner
// entity because the vault tables carry no tenant_id of their own.

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"transport-app/internal/shared"
)

// BlockedEntity represents a blocked driver or vehicle.
type BlockedEntity struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// EntityMetrics represents aggregate statistics for drivers or vehicles.
type EntityMetrics struct {
	Total        int `json:"total"`
	Blocked      int `json:"blocked"`
	ExpiringSoon int `json:"expiring_soon"`
}

// DashboardData represents overview metrics across drivers, vehicles and
// documents (legacy JSON shape).
type DashboardData struct {
	Drivers          EntityMetrics   `json:"drivers"`
	Vehicles         EntityMetrics   `json:"vehicles"`
	BlockedDrivers   []BlockedEntity `json:"blocked_drivers"`
	BlockedVehicles  []BlockedEntity `json:"blocked_vehicles"`
	DocumentsPending int             `json:"documents_pending"`
}

// DashboardQueries runs the compliance dashboard aggregation.
type DashboardQueries struct {
	db *sql.DB
}

// NewDashboardQueries wires the dashboard queries against the database.
func NewDashboardQueries(db *sql.DB) *DashboardQueries {
	return &DashboardQueries{db: db}
}

// Overview returns the compliance dashboard metrics for tenantID ("" and the
// bootstrap tenant id keep the legacy global view).
func (q *DashboardQueries) Overview(ctx context.Context, tenantID string) (DashboardData, error) {
	if q == nil || q.db == nil {
		return DashboardData{}, fmt.Errorf("database unavailable")
	}
	now := time.Now().Truncate(24 * time.Hour)
	soon := now.Add(7 * 24 * time.Hour)

	var data DashboardData
	data.BlockedDrivers = []BlockedEntity{}
	data.BlockedVehicles = []BlockedEntity{}

	// Tenant isolation: org callers see only their own fleet. Bootstrap
	// ('1'/empty: single-tenant deploys + platform views) keeps global.
	scoped := tenantID != "" && tenantID != string(shared.DefaultTenant) //nolint:tenant-default // bootstrap-vs-org branching, not a data fallback
	var tArg []any
	if scoped {
		tArg = append(tArg, tenantID)
	}

	// Drivers metrics
	driversQ := `SELECT id, license_expiry, status, blocked, COALESCE(blocked_reason,'') FROM drivers`
	if scoped {
		driversQ += ` WHERE tenant_id = $1`
	}
	rows, err := q.db.QueryContext(ctx, driversQ, tArg...)
	if err == nil {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var id, status, bReason string
			var lExp sql.NullString
			var blocked int
			if err := rows.Scan(&id, &lExp, &status, &blocked, &bReason); err == nil {
				data.Drivers.Total++
				isBlocked := blocked == 1 || status == "blocked"
				reason := bReason
				if reason == "" {
					reason = "driver blocked"
				}

				if lExp.Valid && lExp.String != "" {
					if t, err := time.Parse("2006-01-02", lExp.String); err == nil {
						if t.Before(now) {
							isBlocked = true
							reason = fmt.Sprintf("License expired %s", lExp.String)
						} else if t.Before(soon) {
							data.Drivers.ExpiringSoon++
						}
					}
				}

				if isBlocked {
					data.Drivers.Blocked++
					data.BlockedDrivers = append(data.BlockedDrivers, BlockedEntity{ID: id, Reason: reason})
				}
			}
		}
	}

	// Vehicles metrics
	vehiclesQ := `SELECT id, insurance_expiry, fitness_expiry, permit_expiry, COALESCE(CAST(puc_expiry AS TEXT), ''), status, blocked, COALESCE(blocked_reason,'') FROM vehicles`
	if scoped {
		vehiclesQ += ` WHERE tenant_id = $1`
	}
	vRows, err := q.db.QueryContext(ctx, vehiclesQ, tArg...)
	if err == nil {
		defer func() { _ = vRows.Close() }()
		for vRows.Next() {
			var id, status, bReason string
			var insExp, fitExp, perExp, pucExp sql.NullString
			var blocked int
			if err := vRows.Scan(&id, &insExp, &fitExp, &perExp, &pucExp, &status, &blocked, &bReason); err == nil {
				data.Vehicles.Total++
				isBlocked := blocked == 1 || status == "blocked"
				reason := bReason
				if reason == "" {
					reason = "vehicle blocked"
				}

				for _, item := range []struct {
					name string
					exp  sql.NullString
				}{
					{"Insurance", insExp},
					{"Fitness", fitExp},
					{"Permit", perExp},
					{"PUC", pucExp},
				} {
					if item.exp.Valid && item.exp.String != "" {
						if t, err := time.Parse("2006-01-02", item.exp.String); err == nil {
							if t.Before(now) {
								isBlocked = true
								reason = fmt.Sprintf("%s expired %s", item.name, item.exp.String)
								break
							} else if t.Before(soon) {
								data.Vehicles.ExpiringSoon++
							}
						}
					}
				}

				if isBlocked {
					data.Vehicles.Blocked++
					data.BlockedVehicles = append(data.BlockedVehicles, BlockedEntity{ID: id, Reason: reason})
				}
			}
		}
	}

	// Pending documents count (tenant-scoped via owner entity: the vault
	// tables carry no tenant_id of their own).
	ddocQ := `SELECT count(*) FROM driver_documents WHERE status = 'pending_review'`
	vdocQ := `SELECT count(*) FROM vehicle_documents WHERE status = 'pending_review'`
	if scoped {
		ddocQ += ` AND driver_id IN (SELECT id FROM drivers WHERE tenant_id = $1)`
		vdocQ += ` AND vehicle_id IN (SELECT id FROM vehicles WHERE tenant_id = $1)`
	}
	var dPending, vPending int
	_ = q.db.QueryRowContext(ctx, ddocQ, tArg...).Scan(&dPending)
	_ = q.db.QueryRowContext(ctx, vdocQ, tArg...).Scan(&vPending)
	data.DocumentsPending = dPending + vPending

	return data, nil
}
