package pnl

// Agent/handler-facing P&L snapshot read model, extracted from the founder
// handlers' use of the legacy service container (handlers-end-state ADR,
// Phase 4 increment 3). Fields mirror service.PNLSnapshot exactly (same JSON
// tags) and the SQL mirrors its queries, tenant-scoped.

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"

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

// MoneyStrip is the command center money strip read model.
type MoneyStrip struct {
	Date        string  `json:"date"`
	Revenue     float64 `json:"revenue"`
	Spent       float64 `json:"spent"`
	Receivables float64 `json:"receivables"`
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

// GenerateDailySnapshot generates a pnl_daily row for the date, upserting idempotently.
func (q *SnapshotQueries) GenerateDailySnapshot(ctx context.Context, tenantID string, date time.Time) (*Snapshot, error) {
	if q == nil || q.db == nil {
		return nil, sql.ErrNoRows
	}
	tid, err := shared.RequireTenantOr(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	dateStr := date.Format("2006-01-02")
	revenue, fuelCosts, driverPayouts, maintenance, tollCosts, err := q.dailyTotals(ctx, string(tid), dateStr)
	if err != nil {
		return nil, err
	}

	var tdsDeducted float64
	_ = q.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(ds.tds_amount), 0)
		 FROM driver_settlements ds
		 JOIN trips t ON ds.trip_id = t.id
		 WHERE t.tenant_id = $1 AND DATE(ds.created_at) = $2`,
		string(tid), dateStr).Scan(&tdsDeducted)

	var tripCount int
	_ = q.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM trips WHERE tenant_id = $1 AND DATE(departure_time) = $2`,
		string(tid), dateStr).Scan(&tripCount)

	var vehicleCount int
	_ = q.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM vehicles WHERE tenant_id = $1 AND status = 'active'`,
		string(tid)).Scan(&vehicleCount)

	expenses := fuelCosts + driverPayouts + maintenance + tollCosts
	netProfit := revenue - expenses

	snap := &Snapshot{
		ID:            fmt.Sprintf("pnl-%s", uuid.NewString()[:8]),
		TenantID:      string(tid),
		SnapshotDate:  dateStr,
		Revenue:       revenue,
		Expenses:      expenses,
		FuelCosts:     fuelCosts,
		DriverPayouts: driverPayouts,
		Maintenance:   maintenance,
		TollCosts:     tollCosts,
		TdsDeducted:   tdsDeducted,
		NetProfit:     netProfit,
		TripCount:     tripCount,
		VehicleCount:  vehicleCount,
	}

	_, err = q.db.ExecContext(ctx,
		`INSERT INTO pnl_daily
		   (id, tenant_id, snapshot_date, revenue, expenses, fuel_costs,
		    driver_payouts, maintenance, toll_costs, tds_deducted, net_profit,
		    trip_count, vehicle_count)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		 ON CONFLICT (tenant_id, snapshot_date) DO UPDATE SET
		   revenue       = excluded.revenue,
		   expenses      = excluded.expenses,
		   fuel_costs    = excluded.fuel_costs,
		   driver_payouts= excluded.driver_payouts,
		   maintenance   = excluded.maintenance,
		   toll_costs    = excluded.toll_costs,
		   tds_deducted  = excluded.tds_deducted,
		   net_profit    = excluded.net_profit,
		   trip_count    = excluded.trip_count,
		   vehicle_count = excluded.vehicle_count`,
		snap.ID, snap.TenantID, snap.SnapshotDate, snap.Revenue,
		snap.Expenses, snap.FuelCosts, snap.DriverPayouts,
		snap.Maintenance, snap.TollCosts, snap.TdsDeducted,
		snap.NetProfit, snap.TripCount, snap.VehicleCount)
	if err != nil {
		return snap, err
	}
	return snap, nil
}

func (q *SnapshotQueries) dailyTotals(ctx context.Context, tenantID, dateStr string) (revenue, fuel, driverPayouts, maint, toll float64, err error) {
	if err = q.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(total), 0) FROM invoices
		 WHERE tenant_id = $1 AND payment_status = 'paid' AND DATE(created_at) = $2`,
		tenantID, dateStr).Scan(&revenue); err != nil {
		return 0, 0, 0, 0, 0, fmt.Errorf("pnl revenue read failed: %w", err)
	}

	if err = q.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(de.amount), 0) FROM driver_expenses de
		 WHERE de.tenant_id = $1 AND de.category = 'fuel' AND DATE(de.created_at) = $2
		   AND NOT EXISTS (
		     SELECT 1 FROM settlement_lines sl
		     WHERE sl.ref_id = de.id AND sl.line_type IN ('deduction', 'advances')
		   )`,
		tenantID, dateStr).Scan(&fuel); err != nil {
		return 0, 0, 0, 0, 0, fmt.Errorf("pnl fuel cost read failed: %w", err)
	}

	if err = q.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(ds.net_payout), 0)
		 FROM driver_settlements ds
		 JOIN trips t ON ds.trip_id = t.id
		 WHERE t.tenant_id = $1 AND DATE(ds.created_at) = $2`,
		tenantID, dateStr).Scan(&driverPayouts); err != nil {
		return 0, 0, 0, 0, 0, fmt.Errorf("pnl driver payout read failed: %w", err)
	}

	if err = q.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(cost), 0) FROM maintenance_records
		 WHERE tenant_id = $1 AND DATE(performed_at) = $2`,
		tenantID, dateStr).Scan(&maint); err != nil {
		maint = 0
	}

	if err = q.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(amount), 0) FROM fastag_transactions
		 WHERE tenant_id = $1 AND DATE(txn_timestamp) = $2`,
		tenantID, dateStr).Scan(&toll); err != nil {
		return 0, 0, 0, 0, 0, fmt.Errorf("pnl toll cost read failed: %w", err)
	}

	return revenue, fuel, driverPayouts, maint, toll, nil
}

// GetMoneyStrip returns the tenant's daily money strip metrics for the given timestamp.
func (q *SnapshotQueries) GetMoneyStrip(ctx context.Context, tenantID string, now time.Time) (*MoneyStrip, error) {
	if q == nil || q.db == nil {
		return nil, sql.ErrNoRows
	}
	tid, err := shared.RequireTenantOr(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	tID := string(tid)
	dateStr := now.Format("2006-01-02")
	revenue, fuelCosts, driverPayouts, maintenance, tollCosts, err := q.dailyTotals(ctx, tID, dateStr)
	if err != nil {
		return nil, err
	}

	var receivables float64
	_ = q.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(CASE WHEN i.total - COALESCE(p.paid, 0) > 0 THEN i.total - COALESCE(p.paid, 0) ELSE 0 END), 0)
		 FROM invoices i
		 LEFT JOIN (
		   SELECT invoice_id, SUM(amount) AS paid FROM payments GROUP BY invoice_id
		 ) p ON p.invoice_id = i.id
		 WHERE i.tenant_id = $1`,
		tID).Scan(&receivables)

	return &MoneyStrip{
		Date:        dateStr,
		Revenue:     revenue,
		Spent:       fuelCosts + driverPayouts + maintenance + tollCosts,
		Receivables: receivables,
	}, nil
}
