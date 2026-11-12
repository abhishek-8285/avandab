package domain_test

import (
	"testing"

	"transport-app/internal/customer/domain"
)

func TestCalculateQuote_Validation(t *testing.T) {
	_, err := domain.CalculateQuote("q-1", "t-1", "c-1", "", "Mumbai", "Dry", "TATA_ACE", 500, 100, 30)
	if err == nil {
		t.Fatalf("expected error for empty origin")
	}

	_, err = domain.CalculateQuote("q-1", "t-1", "c-1", "Delhi", "", "Dry", "TATA_ACE", 500, 100, 30)
	if err == nil {
		t.Fatalf("expected error for empty destination")
	}
}

func TestCalculateQuote_Calculations(t *testing.T) {
	tests := []struct {
		name         string
		vehicleType  string
		distanceKm   float64
		ttlMinutes   int
		expectedBase float64
		expectedPerKm float64
	}{
		{
			name:         "2 wheeler short distance",
			vehicleType:  "BIKE",
			distanceKm:   10,
			ttlMinutes:   15,
			expectedBase: 50.0,
			expectedPerKm: 12.0,
		},
		{
			name:         "Tata Ace with toll",
			vehicleType:  "TATA_ACE",
			distanceKm:   120, // >30km -> toll = floor(120/50)*120 = 2*120 = 240
			ttlMinutes:   60,
			expectedBase: 400.0,
			expectedPerKm: 25.0,
		},
		{
			name:         "Heavy Trailer",
			vehicleType:  "TRAILER",
			distanceKm:   200,
			ttlMinutes:   0, // fallback to 30
			expectedBase: 3500.0,
			expectedPerKm: 75.0,
		},
		{
			name:         "Default fallback vehicle type and zero distance",
			vehicleType:  "UNKNOWN_TYPE",
			distanceKm:   0, // fallback to 15.0
			ttlMinutes:   0,
			expectedBase: 500.0,
			expectedPerKm: 28.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := domain.CalculateQuote("q-test", "tenant-1", "cust-1", "Delhi", "Jaipur", "General", tt.vehicleType, 1000, tt.distanceKm, tt.ttlMinutes)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if q.BaseRate != tt.expectedBase {
				t.Errorf("base rate = %v, expected %v", q.BaseRate, tt.expectedBase)
			}
			if q.PerKmRate != tt.expectedPerKm {
				t.Errorf("per km rate = %v, expected %v", q.PerKmRate, tt.expectedPerKm)
			}
			if q.Status != "active" {
				t.Errorf("status = %v, expected active", q.Status)
			}
			if q.TotalPrice <= 0 {
				t.Errorf("total price should be > 0, got %v", q.TotalPrice)
			}
			if q.GSTAmount <= 0 {
				t.Errorf("gst amount should be > 0, got %v", q.GSTAmount)
			}
			expectedSubtotal := q.BaseRate + (q.DistanceKm * q.PerKmRate) + q.EstimatedToll
			if q.Subtotal != expectedSubtotal {
				t.Errorf("subtotal = %v, expected %v", q.Subtotal, expectedSubtotal)
			}
		})
	}
}
