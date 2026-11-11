package application

// Agent-facing operations dashboard read model, extracted from
// internal/agent/tools.go (handlers-end-state ADR, Phase 3). The snapshot
// mirrors the legacy DashboardService card metrics: today's trips by departure
// date, active completion-derived count, available drivers/vehicles, pending
// invoices and this month's payment revenue.

import (
	"context"
	"database/sql"
	"time"

	"transport-app/internal/shared"
)

// AgentDashboard is the get_dashboard JSON shape.
type AgentDashboard struct {
	TodayTripsCount   int64   `json:"today_trips_count"`
	ActiveTrips       int64   `json:"active_trips"`
	CompletedTrips    int64   `json:"completed_trips"`
	CancelledTrips    int64   `json:"cancelled_trips"`
	AvailableDrivers  int64   `json:"available_drivers"`
	AvailableVehicles int64   `json:"available_vehicles"`
	PendingPayments   int64   `json:"pending_payments"`
	MonthlyRevenue    float64 `json:"monthly_revenue"`
}

// AgentDashboardQueries is the dashboard facade. Stateless.
type AgentDashboardQueries struct{ db *sql.DB }

// NewAgentDashboardQueries wires the facade against the database.
func NewAgentDashboardQueries(db *sql.DB) *AgentDashboardQueries {
	return &AgentDashboardQueries{db: db}
}

// Snapshot aggregates the eight card metrics for the tenant.
func (q *AgentDashboardQueries) Snapshot(ctx context.Context, tenantID shared.TenantID) (*AgentDashboard, error) {
	if q == nil || q.db == nil {
		return nil, sql.ErrNoRows
	}
	tenant := string(tenantID)
	var d AgentDashboard
	today := time.Now().Format("2006-01-02")

	statusCounts, err := q.tripStatusCounts(ctx, today, tenant)
	if err != nil {
		return nil, err
	}
	for _, n := range statusCounts {
		d.TodayTripsCount += n
	}
	d.CompletedTrips = statusCounts["completed"]
	d.CancelledTrips = statusCounts["cancelled"]
	d.ActiveTrips = d.TodayTripsCount - d.CompletedTrips - d.CancelledTrips - statusCounts["draft"]

	if err := q.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM vehicles WHERE status = 'available' AND tenant_id = ?`, tenant).Scan(&d.AvailableVehicles); err != nil {
		return nil, err
	}
	if err := q.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM drivers WHERE status = 'available' AND tenant_id = ?`, tenant).Scan(&d.AvailableDrivers); err != nil {
		return nil, err
	}
	if err := q.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM invoices WHERE payment_status IN ('pending','partially_paid') AND tenant_id = ?`, tenant).Scan(&d.PendingPayments); err != nil {
		return nil, err
	}
	month := time.Now().Format("2006-01")
	if err := q.db.QueryRowContext(ctx, `SELECT CAST(COALESCE(SUM(amount), 0) AS REAL) FROM payments WHERE tenant_id = ? AND substr(CAST(payment_date AS TEXT), 1, 7) = ?`, tenant, month).Scan(&d.MonthlyRevenue); err != nil {
		return nil, err
	}
	return &d, nil
}

// tripStatusCounts returns today's trip counts by status for the tenant.
func (q *AgentDashboardQueries) tripStatusCounts(ctx context.Context, today, tenant string) (map[string]int64, error) {
	rows, err := q.db.QueryContext(ctx, `
		SELECT status, COUNT(*) FROM trips
		WHERE substr(CAST(departure_time AS TEXT), 1, 10) = ? AND tenant_id = ?
		GROUP BY status`, today, tenant)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	counts := map[string]int64{}
	for rows.Next() {
		var status string
		var n int64
		if err := rows.Scan(&status, &n); err != nil {
			return nil, err
		}
		counts[status] = n
	}
	return counts, rows.Err()
}
