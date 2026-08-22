package aggregate

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"transport-app/internal/shared"
)

func TestNewVehicleAggregate(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	tenantID := shared.TenantID("tenant-1")
	mileage := 12345.67

	agg := NewVehicleAggregate(
		"veh-123",
		tenantID,
		"MH01AB1234",
		"VN-001",
		VehicleTypeTruck,
		10000,
		FuelTypeDiesel,
		now.Add(365*24*time.Hour),
		now.Add(365*24*time.Hour),
		now.Add(365*24*time.Hour),
		VehicleAvailable,
		&mileage,
		now,
	)

	require.NotNil(t, agg)
	assert.Equal(t, VehicleID("veh-123"), agg.ID)
	assert.Equal(t, tenantID, agg.TenantID)
	assert.Equal(t, "MH01AB1234", agg.RegistrationNumber)
	assert.Equal(t, "VN-001", agg.VehicleNumber)
	assert.Equal(t, VehicleTypeTruck, agg.VehicleType)
	assert.Equal(t, int64(10000), agg.Capacity)
	assert.Equal(t, FuelTypeDiesel, agg.FuelType)
	assert.Equal(t, VehicleAvailable, agg.Status)
	require.NotNil(t, agg.CurrentMileage)
	assert.InDelta(t, 12345.67, *agg.CurrentMileage, 0.001)
	assert.Equal(t, now, agg.CreatedAt)
	assert.Equal(t, now, agg.UpdatedAt)
	assert.Len(t, agg.Events(), 1)
	ev, ok := agg.Events()[0].(VehicleCreatedEvent)
	require.True(t, ok)
	assert.Equal(t, VehicleID("veh-123"), ev.ID)
	assert.Equal(t, tenantID, ev.TenantID)
	assert.Equal(t, "MH01AB1234", ev.RegistrationNumber)
	assert.Equal(t, "VN-001", ev.VehicleNumber)
	assert.Equal(t, now, ev.CreatedAt)
}

func TestNewVehicleAggregate_NilMileage(t *testing.T) {
	now := time.Now()
	agg := NewVehicleAggregate(
		"v1", shared.TenantID("1"),
		"REG1", "VN1",
		VehicleTypeBus, 40, FuelTypeCNG,
		now, now, now,
		VehicleAvailable, nil, now,
	)
	require.Nil(t, agg.CurrentMileage)
	assert.Len(t, agg.Events(), 1)
}

func TestVehicleAggregate_EventsAndClearEvents(t *testing.T) {
	now := time.Now()
	agg := NewVehicleAggregate("v1", "1", "REG", "VN", VehicleTypeVan, 10, FuelTypePetrol, now, now, now, VehicleAvailable, nil, now)
	require.Len(t, agg.Events(), 1)
	agg.ClearEvents()
	assert.Len(t, agg.Events(), 0)
	assert.Nil(t, agg.Events())

	// After clear, update should produce new event
	newMileage := 500.0
	err := agg.UpdateDetails("REG2", "VN2", VehicleTypePickup, 20, FuelTypeDiesel, now, now, now, VehicleRunning, &newMileage, now.Add(time.Hour))
	require.NoError(t, err)
	require.Len(t, agg.Events(), 1)
	_, ok := agg.Events()[0].(VehicleUpdatedEvent)
	assert.True(t, ok)

	agg.ClearEvents()
	assert.Len(t, agg.Events(), 0)
}

func TestVehicleAggregate_UpdateDetails_Success(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	later := now.Add(2 * time.Hour)
	mileage1 := 100.0
	mileage2 := 200.5

	agg := NewVehicleAggregate("v1", shared.TenantID("t1"), "REG1", "VN1", VehicleTypeTruck, 5000, FuelTypeDiesel, now, now, now, VehicleAvailable, &mileage1, now)
	agg.ClearEvents()

	insurance := now.Add(200 * 24 * time.Hour)
	fitness := now.Add(300 * 24 * time.Hour)
	permit := now.Add(400 * 24 * time.Hour)

	err := agg.UpdateDetails("MH02CD5678", "VN-999", VehicleTypeBus, 9999, FuelTypeElectric, insurance, fitness, permit, VehicleMaintenance, &mileage2, later)
	require.NoError(t, err)

	assert.Equal(t, "MH02CD5678", agg.RegistrationNumber)
	assert.Equal(t, "VN-999", agg.VehicleNumber)
	assert.Equal(t, VehicleTypeBus, agg.VehicleType)
	assert.Equal(t, int64(9999), agg.Capacity)
	assert.Equal(t, FuelTypeElectric, agg.FuelType)
	assert.Equal(t, insurance, agg.InsuranceExpiry)
	assert.Equal(t, fitness, agg.FitnessExpiry)
	assert.Equal(t, permit, agg.PermitExpiry)
	assert.Equal(t, VehicleMaintenance, agg.Status)
	require.NotNil(t, agg.CurrentMileage)
	assert.InDelta(t, 200.5, *agg.CurrentMileage, 0.001)
	assert.Equal(t, later, agg.UpdatedAt)
	// CreatedAt unchanged
	assert.Equal(t, now, agg.CreatedAt)

	require.Len(t, agg.Events(), 1)
	ev, ok := agg.Events()[0].(VehicleUpdatedEvent)
	require.True(t, ok)
	assert.Equal(t, VehicleID("v1"), ev.ID)
	assert.Equal(t, shared.TenantID("t1"), ev.TenantID)
	assert.Equal(t, VehicleMaintenance, ev.Status)
	assert.Equal(t, "MH02CD5678", ev.RegistrationNumber)
	assert.Equal(t, later, ev.UpdatedAt)
}

func TestVehicleAggregate_UpdateDetails_NilMileage(t *testing.T) {
	now := time.Now()
	mileage := 10.0
	agg := NewVehicleAggregate("v1", "1", "REG", "VN", VehicleTypeTruck, 10, FuelTypeDiesel, now, now, now, VehicleAvailable, &mileage, now)
	agg.ClearEvents()
	err := agg.UpdateDetails("REG2", "VN2", VehicleTypeTruck, 10, FuelTypeDiesel, now, now, now, VehicleAvailable, nil, now.Add(time.Hour))
	require.NoError(t, err)
	assert.Nil(t, agg.CurrentMileage)
	require.Len(t, agg.Events(), 1)
}

func TestVehicleAggregate_UpdateDetails_ValidationError(t *testing.T) {
	now := time.Now()
	agg := NewVehicleAggregate("v1", "1", "REG", "VN", VehicleTypeTruck, 10, FuelTypeDiesel, now, now, now, VehicleAvailable, nil, now)

	tests := []struct {
		name    string
		reg     string
		vehNum  string
		wantErr string
	}{
		{"empty registration", "", "VN2", "registration and vehicle number are required"},
		{"empty vehicle number", "REG2", "", "registration and vehicle number are required"},
		{"both empty", "", "", "registration and vehicle number are required"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			agg.ClearEvents()
			err := agg.UpdateDetails(tc.reg, tc.vehNum, VehicleTypeTruck, 10, FuelTypeDiesel, now, now, now, VehicleAvailable, nil, now)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
			// No event should be added on error
			assert.Len(t, agg.Events(), 0)
		})
	}
}

func TestVehicleAggregate_Constants(t *testing.T) {
	assert.Equal(t, VehicleType("truck"), VehicleTypeTruck)
	assert.Equal(t, VehicleType("mini_truck"), VehicleTypeMiniTruck)
	assert.Equal(t, VehicleType("bus"), VehicleTypeBus)
	assert.Equal(t, VehicleType("van"), VehicleTypeVan)
	assert.Equal(t, VehicleType("pickup"), VehicleTypePickup)
	assert.Equal(t, VehicleType("tempo"), VehicleTypeTempo)

	assert.Equal(t, FuelType("diesel"), FuelTypeDiesel)
	assert.Equal(t, FuelType("petrol"), FuelTypePetrol)
	assert.Equal(t, FuelType("gas"), FuelTypeGas)
	assert.Equal(t, FuelType("electric"), FuelTypeElectric)
	assert.Equal(t, FuelType("cng"), FuelTypeCNG)

	assert.Equal(t, VehicleStatus("available"), VehicleAvailable)
	assert.Equal(t, VehicleStatus("running"), VehicleRunning)
	assert.Equal(t, VehicleStatus("maintenance"), VehicleMaintenance)
	assert.Equal(t, VehicleStatus("inactive"), VehicleInactive)
}
