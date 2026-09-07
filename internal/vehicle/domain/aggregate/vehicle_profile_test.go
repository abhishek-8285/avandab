package aggregate

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"transport-app/internal/shared"
)

func TestVehicleProfile_ApplyDefaults(t *testing.T) {
	now := time.Now()
	v := NewVehicleAggregate("v1", shared.TenantID("t1"), "REG", "VN",
		VehicleTypeTruck, 10, FuelTypeDiesel, now, now, now, VehicleAvailable, nil, now)

	require.NoError(t, v.ApplyProfile(VehicleProfile{Manufacturer: "TATA"}, now))
	assert.Equal(t, FleetClassCV, v.Profile.FleetClass)
	assert.Equal(t, OwnershipOwn, v.Profile.Ownership)
	assert.Equal(t, "INR", v.Profile.AcquisitionCurrency)
	assert.Equal(t, "TO", v.Profile.WeightUnit)
	assert.Equal(t, "TATA", v.Profile.Manufacturer)
	// ApplyProfile records no event of its own (create event only).
	require.Len(t, v.Events(), 1)
}

func TestVehicleProfile_ValidationError(t *testing.T) {
	now := time.Now()
	v := NewVehicleAggregate("v1", shared.TenantID("t1"), "REG", "VN",
		VehicleTypeTruck, 10, FuelTypeDiesel, now, now, now, VehicleAvailable, nil, now)

	for _, p := range []VehicleProfile{
		{FleetClass: "XX"},
		{Ownership: "Z"},
		{WeightUnit: "LB"},
	} {
		require.Error(t, v.ApplyProfile(p, now))
	}
	assert.Equal(t, FleetClass(""), v.Profile.FleetClass, "failed apply must not mutate")
}

func TestVehicleProfile_FuelStation(t *testing.T) {
	now := time.Now()
	v := NewVehicleAggregate("v1", shared.TenantID("t1"), "BLR-FST-42", "FST-42",
		VehicleTypeTruck, 0, FuelTypeDiesel, now, now, now, VehicleAvailable, nil, now)

	require.NoError(t, v.ApplyProfile(VehicleProfile{
		FleetClass: FleetClassFS, Ownership: OwnershipFuel, FacilityID: "MM21000000757",
	}, now))
	assert.Equal(t, FleetClassFS, v.Profile.FleetClass)
	assert.Equal(t, OwnershipFuel, v.Profile.Ownership)
}

func TestVehicleConstants_SOPParity(t *testing.T) {
	assert.Equal(t, VehicleStatus("blocked"), VehicleBlocked)
	assert.Equal(t, FleetClass("CV"), FleetClassCV)
	assert.Equal(t, FleetClass("OFS"), FleetClassOFS)
	assert.Equal(t, Ownership("O"), OwnershipOwn)
	assert.Equal(t, Ownership("M"), OwnershipMachine)
}
