package pnl

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"transport-app/internal/shared"
)

// LivePnL is the current revenue and cost estimate for one trip.
type LivePnL struct {
	TripID             string    `json:"trip_id"`
	QuotedFare         float64   `json:"quoted_fare"`
	FuelCost           float64   `json:"fuel_cost"`
	FuelCostLow        float64   `json:"fuel_cost_low"`
	FuelCostHigh       float64   `json:"fuel_cost_high"`
	TollCost           float64   `json:"toll_cost"`
	KharchaApproved    float64   `json:"kharcha_approved"`
	FuelConsumedLiters float64   `json:"fuel_consumed_liters"`
	EstimatedMargin    float64   `json:"estimated_margin"`
	MarginPercentage   float64   `json:"margin_percentage"`
	MarginLow          float64   `json:"margin_low"`
	MarginHigh         float64   `json:"margin_high"`
	LowMargin          bool      `json:"low_margin"`
	MarginAvailable    bool      `json:"margin_available"`
	FuelCostStatus     string    `json:"fuel_cost_status"`
	Confidence         string    `json:"confidence"`
	LastUpdate         time.Time `json:"last_update"`
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
		WHERE t.id = ? AND t.tenant_id = ?`, tripID, tenantID).
		Scan(&p.TripID, &p.QuotedFare, &vehicleID, &fuelType, &efficiency)
	if err != nil {
		return LivePnL{}, err
	}

	var startOdometer, latestOdometer sql.NullFloat64
	_ = s.db.QueryRowContext(ctx, `SELECT MIN(odometer), MAX(odometer) FROM telemetry_snapshots WHERE trip_id = ?`, tripID).
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
		WHERE trip_id = ? AND (expense_type = 'toll' OR category = 'toll')
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
		WHERE trip_id = ? AND (COALESCE(status, '') IN ('approved', 'settled') OR approved = 1)
		  AND expense_type <> 'toll' AND COALESCE(category, '') <> 'toll'`+kharchaFuelFilter, tripID).Scan(&p.KharchaApproved)

	p.MarginAvailable = p.FuelCostStatus == "estimated"
	if p.MarginAvailable {
		p.EstimatedMargin = p.QuotedFare - p.FuelCost - p.TollCost - p.KharchaApproved
		p.MarginLow = p.QuotedFare - p.FuelCostHigh - p.TollCost - p.KharchaApproved
		p.MarginHigh = p.QuotedFare - p.FuelCostLow - p.TollCost - p.KharchaApproved
	}
	if p.MarginAvailable && p.QuotedFare > 0 {
		p.MarginPercentage = p.EstimatedMargin / p.QuotedFare * 100
	}
	p.LowMargin = p.MarginAvailable && p.QuotedFare > 0 && p.MarginHigh/p.QuotedFare*100 < 10
	p.LastUpdate = time.Now().UTC()
	_, err = s.db.ExecContext(ctx, `UPDATE trips SET estimated_margin = ?, fuel_consumed_liters = ?, toll_costs = ?, last_pnl_update = ?, fuel_cost_low = ?, fuel_cost_high = ?, margin_low = ?, margin_high = ?, pnl_confidence = ?, fuel_cost_status = ? WHERE id = ? AND tenant_id = ?`,
		p.EstimatedMargin, p.FuelConsumedLiters, p.TollCost, p.LastUpdate, p.FuelCostLow, p.FuelCostHigh, p.MarginLow, p.MarginHigh, p.Confidence, p.FuelCostStatus, tripID, tenantID)
	return p, err
}
