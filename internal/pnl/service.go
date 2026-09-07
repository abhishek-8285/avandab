package pnl

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	appdb "transport-app/internal/database"

	"transport-app/internal/shared"
)

// LivePnL is the current revenue and cost estimate for one trip.
type LivePnL struct {
	TripID                string    `json:"trip_id"`
	QuotedFare            float64   `json:"quoted_fare"`
	FuelCost              float64   `json:"fuel_cost"`
	FuelCostLow           float64   `json:"fuel_cost_low"`
	FuelCostHigh          float64   `json:"fuel_cost_high"`
	TollCost              float64   `json:"toll_cost"`
	KharchaApproved       float64   `json:"kharcha_approved"`
	MaintenanceCost       float64   `json:"maintenance_cost"`
	MaintenanceCostStatus string    `json:"maintenance_cost_status"`
	FuelConsumedLiters    float64   `json:"fuel_consumed_liters"`
	EstimatedMargin       float64   `json:"estimated_margin"`
	MarginPercentage      float64   `json:"margin_percentage"`
	MarginLow             float64   `json:"margin_low"`
	MarginHigh            float64   `json:"margin_high"`
	LowMargin             bool      `json:"low_margin"`
	MarginAvailable       bool      `json:"margin_available"`
	FuelCostStatus        string    `json:"fuel_cost_status"`
	Confidence            string    `json:"confidence"`
	LastUpdate            time.Time `json:"last_update"`
}

type Service struct{ db *sql.DB }

func NewService(db *sql.DB) *Service { return &Service{db: db} }

// Calculate derives P&L from booking fare, telemetry odometer, vehicle efficiency,
// latest fuel price, and approved trip expenses, then stores the snapshot on trips.
func (s *Service) Calculate(ctx context.Context, tripID string) (LivePnL, error) {
	if s == nil || s.db == nil {
		return LivePnL{}, errors.New("pnl database is unavailable")
	}
	tenantID := string(shared.TenantIDFromContext(ctx))
	var p LivePnL
	var vehicleID, fuelType string
	var efficiency sql.NullFloat64
	err := s.db.QueryRowContext(ctx, `
		SELECT t.id, COALESCE(b.price, 0), COALESCE(t.vehicle_id, ''),
		       COALESCE(v.fuel_type, 'diesel'), COALESCE(v.current_mileage, 0)
		FROM trips t
		LEFT JOIN bookings b ON b.id = t.booking_id
		LEFT JOIN vehicles v ON v.id = t.vehicle_id
		LEFT JOIN routes r ON r.id = t.route_id
		WHERE t.id = $1 AND t.tenant_id = $2`, tripID, tenantID).
		Scan(&p.TripID, &p.QuotedFare, &vehicleID, &fuelType, &efficiency)
	if err != nil {
		return LivePnL{}, err
	}

	var startOdometer, latestOdometer sql.NullFloat64
	_ = s.db.QueryRowContext(ctx, `SELECT MIN(odometer), MAX(odometer) FROM telemetry_snapshots WHERE trip_id = $1`, tripID).
		Scan(&startOdometer, &latestOdometer)
	if startOdometer.Valid && latestOdometer.Valid && efficiency.Valid && efficiency.Float64 > 0 {
		if distance := latestOdometer.Float64 - startOdometer.Float64; distance > 0 {
			p.FuelConsumedLiters = distance / efficiency.Float64
		}
	}

	priceColumn := "diesel_price"
	if fuelType == "petrol" {
		priceColumn = "petrol_price"
	}
	uncertainty := 0.15
	if fuelType == "cng" || fuelType == "gas" {
		uncertainty = 0.25
	}
	var fuelPrice sql.NullFloat64
	query := `SELECT ` + priceColumn + ` FROM fuel_prices WHERE tenant_id = ? ORDER BY updated_at DESC LIMIT 1`
	_ = s.db.QueryRowContext(ctx, query, tenantID).Scan(&fuelPrice)
	if fuelPrice.Valid && startOdometer.Valid && latestOdometer.Valid && efficiency.Valid && efficiency.Float64 > 0 {
		p.FuelCost = p.FuelConsumedLiters * fuelPrice.Float64
		p.FuelCostLow = p.FuelCost * (1 - uncertainty)
		p.FuelCostHigh = p.FuelCost * (1 + uncertainty)
		p.FuelCostStatus = "estimated"
		p.Confidence = "medium"
		if fuelType == "cng" || fuelType == "gas" {
			p.Confidence = "low"
		}
	} else {
		p.FuelCostStatus = "pending_verification"
		p.Confidence = "unavailable"
	}
	_ = s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(amount), 0) FROM driver_expenses
		WHERE trip_id = $1 AND (expense_type = 'toll' OR category = 'toll')
		  AND (COALESCE(status, '') IN ('approved', 'settled') OR approved = 1)`, tripID).Scan(&p.TollCost)
	// Approved non-toll expenses. When telemetry produced a real fuel-cost
	// estimate, approved FUEL claims are excluded — they describe the same
	// spend as FuelCost and would otherwise be counted twice in margin.
	kharchaFuelFilter := ""
	if p.FuelCostStatus == "estimated" {
		kharchaFuelFilter = " AND COALESCE(expense_type, '') <> 'fuel' AND COALESCE(category, '') <> 'fuel'"
	}
	_ = s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(amount), 0) FROM driver_expenses
		WHERE trip_id = $1 AND (COALESCE(status, '') IN ('approved', 'settled') OR approved = 1)
		  AND expense_type <> 'toll' AND COALESCE(category, '') <> 'toll'`+kharchaFuelFilter, tripID).Scan(&p.KharchaApproved)

	p.MaintenanceCost, p.MaintenanceCostStatus = s.fetchMaintenanceCost(ctx, tenantID, vehicleID, tripID)

	p.MarginAvailable = p.FuelCostStatus == "estimated"
	if p.MarginAvailable {
		p.EstimatedMargin = p.QuotedFare - p.FuelCost - p.TollCost - p.KharchaApproved - p.MaintenanceCost
		p.MarginLow = p.QuotedFare - p.FuelCostHigh - p.TollCost - p.KharchaApproved - p.MaintenanceCost
		p.MarginHigh = p.QuotedFare - p.FuelCostLow - p.TollCost - p.KharchaApproved - p.MaintenanceCost
	}
	if p.MarginAvailable && p.QuotedFare > 0 {
		p.MarginPercentage = p.EstimatedMargin / p.QuotedFare * 100
	}
	p.LowMargin = p.MarginAvailable && p.QuotedFare > 0 && p.MarginHigh/p.QuotedFare*100 < 10
	p.LastUpdate = time.Now().UTC()
	_, err = s.db.ExecContext(ctx, `UPDATE trips SET estimated_margin = $1, fuel_consumed_liters = $2, toll_costs = $3, last_pnl_update = $4, fuel_cost_low = $5, fuel_cost_high = $6, margin_low = $7, margin_high = $8, pnl_confidence = $9, fuel_cost_status = $10 WHERE id = $11 AND tenant_id = $12`,
		p.EstimatedMargin, p.FuelConsumedLiters, p.TollCost, p.LastUpdate, p.FuelCostLow, p.FuelCostHigh, p.MarginLow, p.MarginHigh, p.Confidence, p.FuelCostStatus, tripID, tenantID)
	return p, err
}

// fetchMaintenanceCost sums maintenance_records.cost for the trip's vehicle
// within the trip window (departure_time → arrival_time when known,
// otherwise all records for the vehicle). Tenant comes from the caller
// (shared.TenantIDFromContext) — never a hardcoded literal.
// Returns (0, "unavailable") when the vehicle is unknown or the
// maintenance table cannot be read (e.g. schemas without the table);
// otherwise (sum, "included") — including a zero sum on success.
func (s *Service) fetchMaintenanceCost(ctx context.Context, tenantID, vehicleID, tripID string) (float64, string) {
	if vehicleID == "" || tenantID == "" {
		return 0, "unavailable"
	}
	// Trip window: tolerate schemas without departure/arrival columns
	// (unit-test DBs) — fall back to a vehicle-only sum.
	var windowStart, windowEnd string
	var dep, arr sql.NullString
	if err := s.db.QueryRowContext(ctx,
		`SELECT departure_time, arrival_time FROM trips WHERE id = $1 AND tenant_id = $2`,
		tripID, tenantID).Scan(&dep, &arr); err == nil {
		if dep.Valid && dep.String != "" {
			windowStart = dep.String
		}
		if arr.Valid && arr.String != "" {
			windowEnd = arr.String
		}
	}

	sumQuery := func(tenantScoped bool, withStart, withEnd bool) (string, []interface{}) {
		q := `SELECT COALESCE(SUM(cost), 0) FROM maintenance_records WHERE vehicle_id = ?`
		args := []interface{}{vehicleID}
		if tenantScoped {
			q += ` AND tenant_id = ?`
			args = append(args, tenantID)
		}
		if withStart {
			q += ` AND performed_at >= ?`
			args = append(args, windowStart)
		}
		if withEnd {
			q += ` AND performed_at <= ?`
			args = append(args, windowEnd)
		}
		return q, args
	}

	withStart := windowStart != ""
	withEnd := windowEnd != ""
	var cost sql.NullFloat64
	q, args := sumQuery(true, withStart, withEnd)
	rebound, rerr := appdb.Rebind(q)
	if rerr != nil {
		return 0, "unavailable"
	}
	if err := s.db.QueryRowContext(ctx, rebound, args...).Scan(&cost); err != nil {
		msg := err.Error()
		if strings.Contains(msg, "no such table") || strings.Contains(msg, "does not exist") {
			return 0, "unavailable"
		}
		if strings.Contains(msg, "no such column") || strings.Contains(msg, "unknown column") {
			// Pre-00095 schema without tenant_id, or without performed_at:
			// retry unscoped / windowless before giving up.
			if strings.Contains(msg, "tenant_id") {
				q2, args2 := sumQuery(false, withStart, withEnd)
				rebound2, rerr2 := appdb.Rebind(q2)
				if rerr2 != nil {
					return 0, "unavailable"
				}
				if err2 := s.db.QueryRowContext(ctx, rebound2, args2...).Scan(&cost); err2 != nil {
					return 0, "unavailable"
				}
			} else {
				q2, args2 := sumQuery(true, false, false)
				rebound2, rerr2 := appdb.Rebind(q2)
				if rerr2 != nil {
					return 0, "unavailable"
				}
				if err2 := s.db.QueryRowContext(ctx, rebound2, args2...).Scan(&cost); err2 != nil {
					return 0, "unavailable"
				}
			}
		} else {
			return 0, "unavailable"
		}
	}
	if !cost.Valid {
		return 0, "included"
	}
	return cost.Float64, "included"
}
