package handlers

import (
	"context"
	"strconv"
	"strings"
)

// KPI is one card in the list-page KPI strip (partials/kpi_grid.html).
// Counts/sums are advisory dashboards, not ledger truth — each query is
// tenant-blind because the core tables predate multi-tenancy (matching how
// their list handlers behave).
type KPI struct {
	Label  string
	Key    string // i18n key; template falls back to Label when empty
	Value  string
	Sub    string
	Accent string
}

func (a *App) countByStatus(ctx context.Context, table, column string) map[string]int {
	out := map[string]int{}
	if a.DB == nil {
		return out
	}
	rows, err := a.DB.QueryContext(ctx, "SELECT "+column+", COUNT(*) FROM "+table+" GROUP BY "+column)
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
	// Lakh-style Indian scale: whole rupees below a thousand, k/L/Cr above.
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
	return strings.TrimRight(strconv.FormatFloat(v, 'f', 2, 64), "0.")
}

func i64(n int) string {
	return strconv.FormatInt(int64(n), 10)
}

func (a *App) bookingKPIs(ctx context.Context) []KPI {
	byStatus := a.countByStatus(ctx, "bookings", "status")
	total := 0
	for _, n := range byStatus {
		total += n
	}
	var monthCount int
	var monthValue float64
	if a.DB != nil {
		_ = a.DB.QueryRowContext(ctx,
			`SELECT COUNT(*), COALESCE(SUM(price),0) FROM bookings
			 WHERE created_at >= date('now','localtime','start of month')`).
			Scan(&monthCount, &monthValue)
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

func (a *App) tripKPIs(ctx context.Context) []KPI {
	byStatus := a.countByStatus(ctx, "trips", "status")
	total := 0
	for _, n := range byStatus {
		total += n
	}
	var monthCount int
	if a.DB != nil {
		_ = a.DB.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM trips
			 WHERE created_at >= date('now','localtime','start of month')`).
			Scan(&monthCount)
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

func (a *App) vehicleKPIs(ctx context.Context) []KPI {
	byStatus := a.countByStatus(ctx, "vehicles", "status")
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

func (a *App) driverKPIs(ctx context.Context) []KPI {
	byStatus := a.countByStatus(ctx, "drivers", "status")
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

func (a *App) paymentKPIs(ctx context.Context) []KPI {
	var monthCount, totalCount int
	var monthValue, totalValue float64
	if a.DB != nil {
		_ = a.DB.QueryRowContext(ctx,
			`SELECT COUNT(*), COALESCE(SUM(amount),0) FROM payments
			 WHERE payment_date >= date('now','localtime','start of month')`).
			Scan(&monthCount, &monthValue)
		_ = a.DB.QueryRowContext(ctx,
			`SELECT COUNT(*), COALESCE(SUM(amount),0) FROM payments`).
			Scan(&totalCount, &totalValue)
	}
	return []KPI{
		{Label: "Received This Month", Key: "kpi.payments.month", Value: "₹" + inr(monthValue), Accent: "text-status-success", Sub: i64(monthCount) + " payments"},
		{Label: "All-Time Received", Key: "kpi.payments.alltime", Value: "₹" + inr(totalValue), Sub: i64(totalCount) + " payments"},
	}
}

func (a *App) invoiceKPIs(ctx context.Context) []KPI {
	byStatus := a.countByStatus(ctx, "invoices", "payment_status")
	var raisedValue float64
	count := 0
	for _, n := range byStatus {
		count += n
	}
	if a.DB != nil {
		_ = a.DB.QueryRowContext(ctx, `SELECT COALESCE(SUM(total),0) FROM invoices`).Scan(&raisedValue)
	}
	var outstanding float64
	if a.DB != nil {
		_ = a.DB.QueryRowContext(ctx,
			`SELECT COALESCE(SUM(total - paid_amount),0) FROM invoices WHERE payment_status != 'paid'`).
			Scan(&outstanding)
	}
	return []KPI{
		{Label: "Invoices Raised", Key: "kpi.invoices.raised", Value: i64(count), Sub: "₹" + inr(raisedValue) + " billed"},
		{Label: "Paid", Key: "kpi.invoices.paid", Value: i64(byStatus["paid"]), Accent: "text-status-success"},
		{Label: "Outstanding", Key: "kpi.invoices.outstanding", Value: "₹" + inr(outstanding), Accent: "text-status-alert",
			Sub: i64(byStatus["pending"]+byStatus["partially_paid"]) + " unpaid"},
	}
}
