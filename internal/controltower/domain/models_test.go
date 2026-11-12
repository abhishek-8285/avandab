package domain_test

import (
	"testing"
	"time"

	"transport-app/internal/controltower/domain"
)

func TestControlTowerModels(t *testing.T) {
	now := time.Now()
	trip := domain.ControlTowerTrip{
		TripID:      "trip-1",
		TripNumber:  "TRIP-100",
		TenantID:    "t-1",
		BookingID:   "bk-1",
		Status:      "in_transit",
		StartTime:   &now,
		Origin:      "Delhi Hub",
		Destination: "Jaipur Depot",
		Driver: domain.DriverInfo{
			ID:    "drv-1",
			Name:  "Ramesh Kumar",
			Phone: "+919876543210",
		},
		Vehicle: domain.VehicleInfo{
			ID:                 "veh-1",
			VehicleNumber:      "HR-55-AB-1234",
			RegistrationNumber: "HR55AB1234",
		},
		Progression: domain.ControlTowerProgression{
			TotalStops:        4,
			CompletedStops:    2,
			ProgressPercent:   50.0,
			AllStopsCompleted: false,
		},
		Safety: domain.ControlTowerSafety{
			HasActiveSOS:       false,
			ActiveAlertsCount:  0,
			IsDeviated:         false,
			DeviationDistanceM: 0,
		},
		Stops: []domain.ControlTowerStop{
			{
				ID:           "stop-1",
				TripID:       "trip-1",
				StopSequence: 1,
				StopType:     "pickup",
				Status:       "completed",
			},
			{
				ID:           "stop-2",
				TripID:       "trip-1",
				StopSequence: 2,
				StopType:     "drop",
				Status:       "en_route",
			},
		},
	}

	if trip.TripNumber != "TRIP-100" {
		t.Errorf("expected trip number TRIP-100, got %s", trip.TripNumber)
	}
	if trip.Progression.ProgressPercent != 50.0 {
		t.Errorf("expected 50%% progress, got %v", trip.Progression.ProgressPercent)
	}
	if len(trip.Stops) != 2 {
		t.Errorf("expected 2 stops, got %d", len(trip.Stops))
	}
}
