package pnl

// Agent/handler-facing P&L snapshot read model, extracted from the founder
// handlers' use of the legacy service container (handlers-end-state ADR,
// Phase 4 increment 3). Fields mirror service.PNLSnapshot exactly (same JSON
// tags) and the SQL mirrors its queries, tenant-scoped.

import (
	"context"
	"database/sql"
	"time"

	"transport-app/internal/shared"
)

// Snapshot is one pnl_daily row (legacy-compatible JSON shape).
type Snapshot struct {
	ID            string  `json:"id"`
	TenantID      string  `json:"tenant_id"`
	SnapshotDate  string  `json:"snapshot_date"`
	Revenue       float64 `json:"revenue"`
	Expenses      float64 `json:"expenses"`
	FuelCosts     float64 `json:"fuel_costs"`
	DriverPayouts float64 `json:"driver_payouts"`
	Maintenance   float64 `json:"maintenance"`
	TollCosts     float64 `json:"toll_costs"`
	TdsDeducted   float64 `json:"tds_deducted"`
	NetProfit     float64 `json:"net_profit"`
	TripCount     int     `json:"trip_count"`
	VehicleCount  int     `json:"vehicle_count"`
}

// SnapshotQueries is the read facade. Stateless.
type SnapshotQueries struct{ db *sql.DB }

// NewSnapshotQueries wires the facade against the database.
func NewSnapshotQueries(db *sql.DB) *SnapshotQueries { return &SnapshotQueries{db: db} }

const snapshotColumns = `id, tenant_id, snapshot_date, revenue, expenses, fuel_costs,
	        driver_payouts, maintenance, toll_costs, tds_deducted, net_profit,
	        trip_count, vehicle_count`

// Latest returns the newest snapshot for the tenant (nil when none exist).
func (q *SnapshotQueries) Latest(ctx context.Context, tenantID string) (*Snapshot, error) {
	if q == nil || q.db == nil {
		return nil, sql.ErrNoRows
	}
	tid, err := shared.RequireTenantOr(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	row := q.db.QueryRowContext(ctx, `SELECT `+snapshotColumns+` FROM pnl_daily WHERE tenant_id = $1 ORDER BY snapshot_date DESC LIMIT 1`, string(tid))
	snap, err := scanSnapshot(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &snap, nil
}

// Range returns snapshots for an inclusive date range, oldest first.
func (q *SnapshotQueries) Range(ctx context.Context, tenantID string, from, to time.Time) ([]Snapshot, error) {
	if q == nil || q.db == nil {
		return nil, sql.ErrNoRows
	}
	tid, err := shared.RequireTenantOr(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := q.db.QueryContext(ctx, `SELECT `+snapshotColumns+` FROM pnl_daily
		WHERE tenant_id = $1 AND snapshot_date BETWEEN $2 AND $3
		ORDER BY snapshot_date ASC`,
		string(tid), from.Format("2006-01-02"), to.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Snapshot
	for rows.Next() {
		snap, err := scanSnapshot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, snap)
	}
	return out, rows.Err()
}

func scanSnapshot(row interface{ Scan(...any) error }) (Snapshot, error) {
	var s Snapshot
	err := row.Scan(&s.ID, &s.TenantID, &s.SnapshotDate, &s.Revenue, &s.Expenses, &s.FuelCosts,
		&s.DriverPayouts, &s.Maintenance, &s.TollCosts, &s.TdsDeducted, &s.NetProfit, &s.TripCount, &s.VehicleCount)
	return s, err
}
