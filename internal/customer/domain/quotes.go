package domain

import (
	"errors"
	"math"
	"time"
)

// Quote represents a server-authoritative price calculation with an expiry TTL.
type Quote struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"tenant_id"`
	CustomerID     string    `json:"customer_id"`
	Origin         string    `json:"origin"`
	Destination    string    `json:"destination"`
	CargoType      string    `json:"cargo_type"`
	VehicleType    string    `json:"vehicle_type"`
	WeightKg       float64   `json:"weight_kg"`
	DistanceKm     float64   `json:"distance_km"`
	BaseRate       float64   `json:"base_rate"`
	PerKmRate      float64   `json:"per_km_rate"`
	EstimatedToll  float64   `json:"estimated_toll"`
	Subtotal       float64   `json:"subtotal"`
	GSTRate        float64   `json:"gst_rate"`
	GSTAmount      float64   `json:"gst_amount"`
	DiscountAmount float64   `json:"discount_amount"`
	TotalPrice     float64   `json:"total_price"`
	Status         string    `json:"status"` // active, converted, expired, cancelled
	ExpiresAt      time.Time `json:"expires_at"`
	CreatedAt      time.Time `json:"created_at"`
}

// CalculateQuote performs server-side pricing given freight parameters.
func CalculateQuote(id, tenantID, customerID, origin, destination, cargoType, vehicleType string, weightKg, distanceKm float64, ttlMinutes int) (*Quote, error) {
	if origin == "" || destination == "" {
		return nil, errors.New("origin and destination are required")
	}
	if distanceKm <= 0 {
		distanceKm = 15.0 // fallback minimum distance
	}
	if ttlMinutes <= 0 {
		ttlMinutes = 30
	}

	// Base rate & per-km rate lookup by vehicle type
	var baseRate, perKmRate float64
	switch vehicleType {
	case "2_WHEELER", "BIKE":
		baseRate = 50.0
		perKmRate = 12.0
	case "3_WHEELER", "AUTO":
		baseRate = 150.0
		perKmRate = 18.0
	case "TATA_ACE", "MINI_TRUCK":
		baseRate = 400.0
		perKmRate = 25.0
	case "PICKUP_8FT", "BOLERO":
		baseRate = 700.0
		perKmRate = 32.0
	case "TRUCK_14FT", "TRUCK_17FT":
		baseRate = 1500.0
		perKmRate = 45.0
	case "CONTAINER_20FT", "TRAILER":
		baseRate = 3500.0
		perKmRate = 75.0
	default:
		// Standard small commercial vehicle default
		baseRate = 500.0
		perKmRate = 28.0
	}

	// Toll estimation: approx ₹100 per 50 km on highways for commercial vehicles
	var estimatedToll float64
	if distanceKm > 30 {
		estimatedToll = math.Floor(distanceKm/50.0) * 120.0
	}

	subtotal := baseRate + (distanceKm * perKmRate) + estimatedToll
	gstRate := 0.05 // 5% GST for goods transport agencies (GTA)
	gstAmount := math.Round(subtotal*gstRate*100) / 100
	discountAmount := 0.0
	totalPrice := math.Round((subtotal+gstAmount-discountAmount)*100) / 100

	now := time.Now()
	return &Quote{
		ID:             id,
		TenantID:       tenantID,
		CustomerID:     customerID,
		Origin:         origin,
		Destination:    destination,
		CargoType:      cargoType,
		VehicleType:    vehicleType,
		WeightKg:       weightKg,
		DistanceKm:     distanceKm,
		BaseRate:       baseRate,
		PerKmRate:      perKmRate,
		EstimatedToll:  estimatedToll,
		Subtotal:       subtotal,
		GSTRate:        gstRate,
		GSTAmount:      gstAmount,
		DiscountAmount: discountAmount,
		TotalPrice:     totalPrice,
		Status:         "active",
		ExpiresAt:      now.Add(time.Duration(ttlMinutes) * time.Minute),
		CreatedAt:      now,
	}, nil
}
