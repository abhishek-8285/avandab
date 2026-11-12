// Package kpi owns the dashboard KPI read models (list-page KPI strips).
// HTTP handlers pass the tenant and render; the queries and formatting live
// here so internal/handlers/ stays an adapter layer
// (docs/tech-specs/handlers-end-state-adr.md, Phase 1).
package kpi

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"
)

// KPI is one card in the list-page KPI strip (partials/kpi_grid.html).
// Counts/sums are advisory dashboards, not ledger truth.
type KPI struct {
	Label  string
	Key    string // i18n key; template falls back to Label when empty
	Value  string
	Sub    string
	Accent string
}

// Service runs dashboard KPI queries.
type Service struct {
	db *sql.DB
}

// New builds a KPI read-model service over the app database.
func New(database *sql.DB) *Service {
	return &Service{db: database}
}

// monthStartDay is the server-local first of the current month (YYYY-MM-DD),
// a portable replacement for the old sqlite month-start expression.
func monthStartDay() string {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).Format("2006-01-02")
}

func (s *Service) countByStatus(ctx context.Context, tenantID, table, column string) map[string]int {
	out := map[string]int{}
	if s == nil || s.db == nil {
		return out
	}
	var rows *sql.Rows
	var err error
	if tenantID != "" {
		rows, err = s.db.QueryContext(ctx, "SELECT "+column+", COUNT(*) FROM "+table+" WHERE tenant_id = ? GROUP BY "+column, tenantID)
	} else {
		rows, err = s.db.QueryContext(ctx, "SELECT "+column+", COUNT(*) FROM "+table+" GROUP BY "+column)
	}
	if err != nil {
		return out
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			break
		}
		out[status] = n
	}
	return out
}

func inr(v float64) string {
	switch {
	case v >= 1e7:
		return trimFloat(v/1e7) + " Cr"
	case v >= 1e5:
		return trimFloat(v/1e5) + " L"
	case v >= 1e3:
		return trimFloat(v/1e3) + "k"
	default:
		return trimFloat(v)
	}
}

func trimFloat(v float64) string {
	s := strconv.FormatFloat(v, 'f', 2, 64)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	return s
}

func i64(n int) string {
	return strconv.FormatInt(int64(n), 10)
}

// Bookings returns the bookings list KPI strip.
func (s *Service) Bookings(ctx context.Context, tenantID string) []KPI {
	byStatus := s.countByStatus(ctx, tenantID, "bookings", "status")
	total := 0
	for _, n := range byStatus {
		total += n
	}
	var monthCount int
	var monthValue float64
	if s != nil && s.db != nil {
		if tenantID != "" {
			_ = s.db.QueryRowContext(ctx,
				`SELECT COUNT(*), COALESCE(SUM(price),0) FROM bookings
				 WHERE substr(CAST(created_at AS TEXT), 1, 10) >= ? AND tenant_id = ?`, monthStartDay(), tenantID).
				Scan(&monthCount, &monthValue)
		} else {
			_ = s.db.QueryRowContext(ctx,
				`SELECT COUNT(*), COALESCE(SUM(price),0) FROM bookings
				 WHERE substr(CAST(created_at AS TEXT), 1, 10) >= ?`, monthStartDay()).
				Scan(&monthCount, &monthValue)
		}
	}
	sub := i64(monthCount) + " new this month"
	if monthValue > 0 {
		sub += " · ₹" + inr(monthValue)
	}
	return []KPI{
		{Label: "Total Bookings", Key: "kpi.bookings.total", Value: i64(total), Sub: sub},
		{Label: "Pending", Key: "kpi.bookings.pending", Value: i64(byStatus["pending"]), Accent: "text-status-warning"},
		{Label: "Confirmed", Key: "kpi.bookings.confirmed", Value: i64(byStatus["confirmed"]), Accent: "text-status-info"},
		{Label: "Completed", Key: "kpi.bookings.completed", Value: i64(byStatus["completed"]), Accent: "text-status-success"},
	}
}

// Trips returns the trips list KPI strip.
func (s *Service) Trips(ctx context.Context, tenantID string) []KPI {
	byStatus := s.countByStatus(ctx, tenantID, "trips", "status")
	total := 0
	for _, n := range byStatus {
		total += n
	}
	var monthCount int
	if s != nil && s.db != nil {
		if tenantID != "" {
			_ = s.db.QueryRowContext(ctx,
				`SELECT COUNT(*) FROM trips
				 WHERE substr(CAST(created_at AS TEXT), 1, 10) >= ? AND tenant_id = ?`, monthStartDay(), tenantID).
				Scan(&monthCount)
		} else {
			_ = s.db.QueryRowContext(ctx,
				`SELECT COUNT(*) FROM trips
				 WHERE substr(CAST(created_at AS TEXT), 1, 10) >= ?`, monthStartDay()).
				Scan(&monthCount)
		}
	}
	sub := i64(monthCount) + " created this month"
	active := byStatus["assigned"] + byStatus["started"] + byStatus["in_transit"]
	completed := byStatus["completed"] + byStatus["delivered"]
	return []KPI{
		{Label: "Total Trips", Key: "kpi.trips.total", Value: i64(total), Sub: sub},
		{Label: "Draft", Key: "kpi.trips.draft", Value: i64(byStatus["draft"]), Accent: "text-status-warning"},
		{Label: "Active", Key: "kpi.trips.active", Value: i64(active), Accent: "text-status-info"},
		{Label: "Completed", Key: "kpi.trips.completed", Value: i64(completed), Accent: "text-status-success"},
	}
}

// Vehicles returns the vehicles list KPI strip.
func (s *Service) Vehicles(ctx context.Context, tenantID string) []KPI {
	byStatus := s.countByStatus(ctx, tenantID, "vehicles", "status")
	total := 0
	for _, n := range byStatus {
		total += n
	}
	return []KPI{
		{Label: "Total Vehicles", Key: "kpi.vehicles.total", Value: i64(total)},
		{Label: "Running", Key: "kpi.vehicles.running", Value: i64(byStatus["running"]), Accent: "text-status-success"},
		{Label: "Available", Key: "kpi.vehicles.available", Value: i64(byStatus["available"]), Accent: "text-status-info"},
		{Label: "Maintenance", Key: "kpi.vehicles.maintenance", Value: i64(byStatus["maintenance"]), Accent: "text-status-warning"},
	}
}

// Drivers returns the drivers list KPI strip.
func (s *Service) Drivers(ctx context.Context, tenantID string) []KPI {
	byStatus := s.countByStatus(ctx, tenantID, "drivers", "status")
	total := 0
	for _, n := range byStatus {
		total += n
	}
	return []KPI{
		{Label: "Total Drivers", Key: "kpi.drivers.total", Value: i64(total)},
		{Label: "Available", Key: "kpi.drivers.available", Value: i64(byStatus["available"]), Accent: "text-status-success"},
		{Label: "On Trip", Key: "kpi.drivers.ontrip", Value: i64(byStatus["on_trip"]), Accent: "text-status-info"},
		{Label: "Inactive / Leave", Key: "kpi.drivers.inactive", Value: i64(byStatus["inactive"] + byStatus["leave"]), Accent: "text-text-muted"},
	}
}

// Payments returns the payments list KPI strip.
func (s *Service) Payments(ctx context.Context, tenantID string) []KPI {
	var monthCount, totalCount int
	var monthValue, totalValue float64
	if s != nil && s.db != nil {
		if tenantID != "" {
			_ = s.db.QueryRowContext(ctx,
				`SELECT COUNT(*), COALESCE(SUM(amount),0) FROM payments
				 WHERE substr(CAST(payment_date AS TEXT), 1, 10) >= ? AND tenant_id = ?`, monthStartDay(), tenantID).
				Scan(&monthCount, &monthValue)
			_ = s.db.QueryRowContext(ctx,
				`SELECT COUNT(*), COALESCE(SUM(amount),0) FROM payments WHERE tenant_id = ?`, tenantID).
				Scan(&totalCount, &totalValue)
		} else {
			_ = s.db.QueryRowContext(ctx,
				`SELECT COUNT(*), COALESCE(SUM(amount),0) FROM payments
				 WHERE substr(CAST(payment_date AS TEXT), 1, 10) >= ?`, monthStartDay()).
				Scan(&monthCount, &monthValue)
			_ = s.db.QueryRowContext(ctx,
				`SELECT COUNT(*), COALESCE(SUM(amount),0) FROM payments`).
				Scan(&totalCount, &totalValue)
		}
	}
	return []KPI{
		{Label: "Received This Month", Key: "kpi.payments.month", Value: "₹" + inr(monthValue), Accent: "text-status-success", Sub: i64(monthCount) + " payments"},
		{Label: "All-Time Received", Key: "kpi.payments.alltime", Value: "₹" + inr(totalValue), Sub: i64(totalCount) + " payments"},
	}
}

// Invoices returns the invoices list KPI strip.
func (s *Service) Invoices(ctx context.Context, tenantID string) []KPI {
	byStatus := s.countByStatus(ctx, tenantID, "invoices", "payment_status")
	var raisedValue float64
	count := 0
	for _, n := range byStatus {
		count += n
	}
	if s != nil && s.db != nil {
		if tenantID != "" {
			_ = s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(total),0) FROM invoices WHERE tenant_id = ?`, tenantID).Scan(&raisedValue)
		} else {
			_ = s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(total),0) FROM invoices`).Scan(&raisedValue)
		}
	}
	var outstanding float64
	if s != nil && s.db != nil {
		if tenantID != "" {
			_ = s.db.QueryRowContext(ctx,
				`SELECT COALESCE(SUM(total - paid_amount),0) FROM invoices WHERE payment_status != 'paid' AND tenant_id = ?`, tenantID).
				Scan(&outstanding)
		} else {
			_ = s.db.QueryRowContext(ctx,
				`SELECT COALESCE(SUM(total - paid_amount),0) FROM invoices WHERE payment_status != 'paid'`).
				Scan(&outstanding)
		}
	}
	return []KPI{
		{Label: "Invoices Raised", Key: "kpi.invoices.raised", Value: i64(count), Sub: "₹" + inr(raisedValue) + " billed"},
		{Label: "Paid", Key: "kpi.invoices.paid", Value: i64(byStatus["paid"]), Accent: "text-status-success"},
		{Label: "Outstanding", Key: "kpi.invoices.outstanding", Value: "₹" + inr(outstanding), Accent: "text-status-alert",
			Sub: i64(byStatus["pending"]+byStatus["partially_paid"]) + " unpaid"},
	}
}
