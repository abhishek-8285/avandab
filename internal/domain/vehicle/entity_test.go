package vehicle_test

import (
	"testing"
	"time"

	"transport-app/internal/domain/types"
	"transport-app/internal/domain/vehicle"
)

func TestVehicle_CanAssign_Valid(t *testing.T) {
	vh := vehicle.Vehicle{
		ID:                 types.VehicleID("vh-1"),
		RegistrationNumber: "MH-12-AB-1234",
		RCExpiry:           time.Now().Add(100 * 24 * time.Hour),
		FitnessExpiry:      time.Now().Add(100 * 24 * time.Hour),
		InsuranceExpiry:    time.Now().Add(100 * 24 * time.Hour),
		Status:             vehicle.VehicleAvailable,
	}

	if err := vh.CanAssign(); err != nil {
		t.Fatalf("expected valid vehicle to be assignable, got: %v", err)
	}
}

func TestVehicle_CanAssign_Blocked(t *testing.T) {
	reason := "Brake overhaul required"
	vh := vehicle.Vehicle{
		ID:                 types.VehicleID("vh-2"),
		RegistrationNumber: "MH-12-AB-1235",
		RCExpiry:           time.Now().Add(100 * 24 * time.Hour),
		FitnessExpiry:      time.Now().Add(100 * 24 * time.Hour),
		InsuranceExpiry:    time.Now().Add(100 * 24 * time.Hour),
		Status:             vehicle.VehicleBlocked,
		Blocked:            true,
		BlockedReason:      &reason,
	}

	err := vh.CanAssign()
	if err == nil {
		t.Fatalf("expected error for blocked vehicle, got nil")
	}
}

func TestVehicle_CanAssign_ExpiredRC(t *testing.T) {
	vh := vehicle.Vehicle{
		ID:                 types.VehicleID("vh-3"),
		RegistrationNumber: "MH-12-AB-1236",
		RCExpiry:           time.Now().Add(-24 * time.Hour),
		FitnessExpiry:      time.Now().Add(100 * 24 * time.Hour),
		InsuranceExpiry:    time.Now().Add(100 * 24 * time.Hour),
		Status:             vehicle.VehicleAvailable,
	}

	err := vh.CanAssign()
	if err == nil {
		t.Fatalf("expected compliance hard-block error for expired RC, got nil")
	}
}

func TestVehicle_CanAssign_ExpiredFitness(t *testing.T) {
	vh := vehicle.Vehicle{
		ID:                 types.VehicleID("vh-4"),
		RegistrationNumber: "MH-12-AB-1237",
		RCExpiry:           time.Now().Add(100 * 24 * time.Hour),
		FitnessExpiry:      time.Now().Add(-24 * time.Hour),
		InsuranceExpiry:    time.Now().Add(100 * 24 * time.Hour),
		Status:             vehicle.VehicleAvailable,
	}

	err := vh.CanAssign()
	if err == nil {
		t.Fatalf("expected compliance hard-block error for expired fitness, got nil")
	}
}

func TestVehicle_CanAssign_ExpiredInsurance(t *testing.T) {
	vh := vehicle.Vehicle{
		ID:                 types.VehicleID("vh-5"),
		RegistrationNumber: "MH-12-AB-1238",
		RCExpiry:           time.Now().Add(100 * 24 * time.Hour),
		FitnessExpiry:      time.Now().Add(100 * 24 * time.Hour),
		InsuranceExpiry:    time.Now().Add(-24 * time.Hour),
		Status:             vehicle.VehicleAvailable,
	}

	err := vh.CanAssign()
	if err == nil {
		t.Fatalf("expected compliance hard-block error for expired insurance, got nil")
	}
}

func TestVehicle_CanAssign_RunningStatus(t *testing.T) {

	vh := vehicle.Vehicle{
		ID:                 types.VehicleID("vh-6"),
		RegistrationNumber: "MH-12-AB-1239",
		RCExpiry:           time.Now().Add(100 * 24 * time.Hour),
		FitnessExpiry:      time.Now().Add(100 * 24 * time.Hour),
		InsuranceExpiry:    time.Now().Add(100 * 24 * time.Hour),
		Status:             vehicle.VehicleRunning,
	}

	err := vh.CanAssign()
	if err == nil {
		t.Fatalf("expected error for running vehicle, got nil")
	}
}

func TestVehicle_CanAssign_ExpiredPUC(t *testing.T) {
	puc := time.Now().Add(-24 * time.Hour)
	vh := vehicle.Vehicle{
		ID:                 types.VehicleID("vh-7"),
		RegistrationNumber: "MH-12-AB-1240",
		RCExpiry:           time.Now().Add(100 * 24 * time.Hour),
		FitnessExpiry:      time.Now().Add(100 * 24 * time.Hour),
		InsuranceExpiry:    time.Now().Add(100 * 24 * time.Hour),
		PUCExpiry:          &puc,
		Status:             vehicle.VehicleAvailable,
	}

	err := vh.CanAssign()
	if err == nil {
		t.Fatalf("expected compliance hard-block error for expired PUC, got nil")
	}
}

func TestVehicle_CanAssign_ValidPUC(t *testing.T) {
	puc := time.Now().Add(100 * 24 * time.Hour)
	vh := vehicle.Vehicle{
		ID:                 types.VehicleID("vh-8"),
		RegistrationNumber: "MH-12-AB-1241",
		RCExpiry:           time.Now().Add(100 * 24 * time.Hour),
		FitnessExpiry:      time.Now().Add(100 * 24 * time.Hour),
		InsuranceExpiry:    time.Now().Add(100 * 24 * time.Hour),
		PUCExpiry:          &puc,
		Status:             vehicle.VehicleAvailable,
	}

	if err := vh.CanAssign(); err != nil {
		t.Fatalf("expected no error for valid PUC, got %v", err)
	}
}
