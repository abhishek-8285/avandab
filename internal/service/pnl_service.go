package service

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"transport-app/internal/shared"
)

// PNLService computes and persists daily profit-and-loss snapshots.
// It reads from existing domain tables (invoices, driver_expenses, driver_settlements,
// maintenance_records, fastag_transactions) and upserts into pnl_daily.
// Spec 16 §2 (PNL Engine), §3.1 (DDL for pnl_daily).
type PNLService struct {
	db             *sql.DB
	founderSignals *FounderSignalsService
}

// SetFounderSignals wires the founder signals service to emit cash-flow alerts.
func (s *PNLService) SetFounderSignals(f *FounderSignalsService) {
	s.founderSignals = f
}

// NewPNLService creates a PNLService backed by the given database.
func NewPNLService(db *sql.DB) *PNLService {
	return &PNLService{db: db}
}

// PNLSnapshot is the computed P&L for one tenant-day.
type PNLSnapshot struct {
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

// GenerateDailySnapshot computes P&L for a given date and upserts the result.
// Idempotent: repeated calls for the same (tenantID, date) UPDATE in place.
// Called by the nightly cron goroutine or via POST /api/v1/pnl/generate.
func (s *PNLService) GenerateDailySnapshot(ctx context.Context, tenantID string, date time.Time) (*PNLSnapshot, error) {
	dateStr := date.Format("2006-01-02")

	// Revenue: sum of paid invoices dated on this day.
	var revenue float64
	_ = s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(total), 0) FROM invoices
		 WHERE tenant_id = ? AND payment_status = 'paid' AND DATE(created_at) = ?`,
		tenantID, dateStr).Scan(&revenue)

	// Fuel costs: standalone fuel-category driver expenses only.
	// Claims already absorbed into a settlement (settlement_lines refs the
	// expense id as deduction/advances) are excluded — their cost is inside
	// driverPayouts (net_payout) below; counting both would double-book.
	var fuelCosts float64
	_ = s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(de.amount), 0) FROM driver_expenses de
		 WHERE de.tenant_id = ? AND de.category = 'fuel' AND DATE(de.created_at) = ?
		   AND NOT EXISTS (
		     SELECT 1 FROM settlement_lines sl
		     WHERE sl.ref_id = de.id AND sl.line_type IN ('deduction', 'advances')
		   )`,
		tenantID, dateStr).Scan(&fuelCosts)

	// Driver payouts: net_payout from driver_settlements.
	var driverPayouts float64
	_ = s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(ds.net_payout), 0)
		 FROM driver_settlements ds
		 JOIN trips t ON ds.trip_id = t.id
		 WHERE t.tenant_id = ? AND DATE(ds.created_at) = ?`,
		tenantID, dateStr).Scan(&driverPayouts)

	// Maintenance costs.
	var maintenance float64
	_ = s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(cost), 0) FROM maintenance_records
		 WHERE tenant_id = ? AND DATE(performed_at) = ?`,
		tenantID, dateStr).Scan(&maintenance)

	// Toll costs from FASTag transactions.
	var tollCosts float64
	_ = s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(amount), 0) FROM fastag_transactions
		 WHERE tenant_id = ? AND DATE(txn_timestamp) = ?`,
		tenantID, dateStr).Scan(&tollCosts)

	// TDS deducted from settlements.
	var tdsDeducted float64
	_ = s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(ds.tds_amount), 0)
		 FROM driver_settlements ds
		 JOIN trips t ON ds.trip_id = t.id
		 WHERE t.tenant_id = ? AND DATE(ds.created_at) = ?`,
		tenantID, dateStr).Scan(&tdsDeducted)

	// Trips departed on this day.
	var tripCount int
	_ = s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM trips WHERE tenant_id = ? AND DATE(departure_time) = ?`,
		tenantID, dateStr).Scan(&tripCount)

	// Active vehicle count (point-in-time).
	var vehicleCount int
	_ = s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM vehicles WHERE tenant_id = ? AND status = 'active'`,
		tenantID).Scan(&vehicleCount)

	expenses := fuelCosts + driverPayouts + maintenance + tollCosts
	netProfit := revenue - expenses

	snap := &PNLSnapshot{
		ID:            generateDisplayID("pnl"),
		TenantID:      tenantID,
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

	// Upsert — idempotent on (tenant_id, snapshot_date).
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO pnl_daily
		   (id, tenant_id, snapshot_date, revenue, expenses, fuel_costs,
		    driver_payouts, maintenance, toll_costs, tds_deducted, net_profit,
		    trip_count, vehicle_count)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(tenant_id, snapshot_date) DO UPDATE SET
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

	// Founder signal: cash flow alert when net profit is negative (Spec 16 §6).
	if snap.NetProfit < 0 && s.founderSignals != nil {
		_, _ = s.founderSignals.EmitCashFlowAlert(ctx, tenantID, snap.NetProfit, snap.SnapshotDate)
	}

	return snap, nil
}

// GetLatest returns the most recent P&L snapshot for a tenant, or nil if none.
func (s *PNLService) GetLatest(ctx context.Context, tenantID string) (*PNLSnapshot, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database unavailable")
	}
	if tenantID == "" {
		tenantID = string(shared.DefaultTenant)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tenant_id, snapshot_date, revenue, expenses, fuel_costs,
		        driver_payouts, maintenance, toll_costs, tds_deducted, net_profit,
		        trip_count, vehicle_count
		 FROM pnl_daily WHERE tenant_id = ? ORDER BY snapshot_date DESC LIMIT 1`,
		tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, nil
	}
	var snap PNLSnapshot
	if err := rows.Scan(&snap.ID, &snap.TenantID, &snap.SnapshotDate,
		&snap.Revenue, &snap.Expenses, &snap.FuelCosts, &snap.DriverPayouts,
		&snap.Maintenance, &snap.TollCosts, &snap.TdsDeducted,
		&snap.NetProfit, &snap.TripCount, &snap.VehicleCount); err != nil {
		return nil, err
	}
	return &snap, rows.Err()
}

// GetPNLRange returns P&L snapshots for an inclusive date range, oldest first.
func (s *PNLService) GetPNLRange(ctx context.Context, tenantID string, from, to time.Time) ([]PNLSnapshot, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tenant_id, snapshot_date, revenue, expenses, fuel_costs,
		        driver_payouts, maintenance, toll_costs, tds_deducted, net_profit,
		        trip_count, vehicle_count
		 FROM pnl_daily
		 WHERE tenant_id = ? AND snapshot_date BETWEEN ? AND ?
		 ORDER BY snapshot_date ASC`,
		tenantID, from.Format("2006-01-02"), to.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PNLSnapshot
	for rows.Next() {
		var snap PNLSnapshot
		if err := rows.Scan(&snap.ID, &snap.TenantID, &snap.SnapshotDate,
			&snap.Revenue, &snap.Expenses, &snap.FuelCosts, &snap.DriverPayouts,
			&snap.Maintenance, &snap.TollCosts, &snap.TdsDeducted,
			&snap.NetProfit, &snap.TripCount, &snap.VehicleCount); err != nil {
			return nil, err
		}
		out = append(out, snap)
	}
	return out, rows.Err()
}

// GetActiveTenantIDs returns all distinct tenant_id values seen in the trips table.
// Used by the nightly cron to snapshot every tenant without a hard-coded list.
func GetActiveTenantIDs(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT DISTINCT tenant_id FROM trips WHERE tenant_id != ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		// Fallback: default single-tenant deployment.
		ids = []string{string(shared.DefaultTenant)}
	}
	return ids, rows.Err()
}
