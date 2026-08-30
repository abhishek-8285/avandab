package domain

import (
	"math"
	"time"
)

// Settlement represents the final financial calculation for a completed trip.
type Settlement struct {
	ID                string    `json:"id"`
	TenantID          string    `json:"tenant_id"`
	TripID            string    `json:"trip_id"`
	DriverID          string    `json:"driver_id"`
	GrossFare         float64   `json:"gross_fare"`
	CommissionRate    float64   `json:"commission_rate"` // e.g. 0.10 (10%)
	CommissionAmount  float64   `json:"commission_amount"`
	TollAdjustment    float64   `json:"toll_adjustment"`
	AdvanceDeductions float64   `json:"advance_deductions"`
	TDSRate           float64   `json:"tds_rate"` // e.g. 0.01 (1% under Sec 194C)
	TDSAmount         float64   `json:"tds_amount"`
	NetPayout         float64   `json:"net_payout"`
	Status            string    `json:"status"` // pending, calculated, approved, payable, paid
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// CalculateSettlement determines deterministic payouts given fare and adjustments.
func CalculateSettlement(id, tenantID, tripID, driverID string, grossFare, tollAdjustment, advanceDeductions, commissionRate, tdsRate float64) *Settlement {
	if commissionRate <= 0 {
		commissionRate = 0.10 // 10% standard platform commission
	}
	if tdsRate <= 0 {
		tdsRate = 0.01 // 1% Indian TDS under Section 194C for individual transporters
	}

	commissionAmount := math.Round(grossFare*commissionRate*100) / 100
	tdsAmount := math.Round((grossFare-commissionAmount)*tdsRate*100) / 100
	netPayout := math.Round((grossFare-commissionAmount+tollAdjustment-advanceDeductions-tdsAmount)*100) / 100

	if netPayout < 0 {
		netPayout = 0
	}

	now := time.Now()
	return &Settlement{
		ID:                id,
		TenantID:          tenantID,
		TripID:            tripID,
		DriverID:          driverID,
		GrossFare:         grossFare,
		CommissionRate:    commissionRate,
		CommissionAmount:  commissionAmount,
		TollAdjustment:    tollAdjustment,
		AdvanceDeductions: advanceDeductions,
		TDSRate:           tdsRate,
		TDSAmount:         tdsAmount,
		NetPayout:         netPayout,
		Status:            "pending",
		CreatedAt:         now,
		UpdatedAt:         now,
	}
}
