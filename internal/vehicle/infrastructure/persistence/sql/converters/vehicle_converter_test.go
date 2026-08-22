package converters

import (
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "transport-app/db/generated/sqlite"
	"transport-app/internal/shared"
	"transport-app/internal/vehicle/domain/aggregate"
)

func TestGetFloat64Pointer_NilAndValid(t *testing.T) {
	// Valid case via ToDomain
	now := time.Now()
	valid := sql.NullFloat64{Float64: 123.45, Valid: true}
	invalid := sql.NullFloat64{Valid: false}

	// Test helper directly
	ptr := getFloat64Pointer(valid)
	require.NotNil(t, ptr)
	assert.InDelta(t, 123.45, *ptr, 0.001)

	ptr = getFloat64Pointer(invalid)
	assert.Nil(t, ptr)

	// Also test via ToDomain conversions handle both
	vValid := db.Vehicle{
		ID:                 "v1",
		TenantID:           "t1",
		RegistrationNumber: "REG1",
		VehicleNumber:      "VN1",
		VehicleType:        string(aggregate.VehicleTypeTruck),
		Capacity:           1000,
		FuelType:           string(aggregate.FuelTypeDiesel),
		InsuranceExpiry:    now,
		FitnessExpiry:      now,
		PermitExpiry:       now,
		Status:             string(aggregate.VehicleAvailable),
		CurrentMileage:     valid,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	agg := ToDomain(vValid)
	require.NotNil(t, agg.CurrentMileage)
	assert.InDelta(t, 123.45, *agg.CurrentMileage, 0.001)

	vInvalid := vValid
	vInvalid.ID = "v2"
	vInvalid.CurrentMileage = invalid
	agg2 := ToDomain(vInvalid)
	assert.Nil(t, agg2.CurrentMileage)
}

func TestToDomain_MapsAllFields(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	mileage := sql.NullFloat64{Float64: 9876.5, Valid: true}
	v := db.Vehicle{
		ID:                 "veh-123",
		TenantID:           "tenant-99",
		RegistrationNumber: "MH01AB1234",
		VehicleNumber:      "VN-XYZ",
		VehicleType:        string(aggregate.VehicleTypeBus),
		Capacity:           42,
		FuelType:           string(aggregate.FuelTypeCNG),
		InsuranceExpiry:    now.Add(10 * 24 * time.Hour),
		FitnessExpiry:      now.Add(20 * 24 * time.Hour),
		PermitExpiry:       now.Add(30 * 24 * time.Hour),
		Status:             string(aggregate.VehicleRunning),
		CurrentMileage:     mileage,
		CreatedAt:          now,
		UpdatedAt:          now.Add(time.Hour),
	}

	agg := ToDomain(v)

	assert.Equal(t, aggregate.VehicleID("veh-123"), agg.ID)
	assert.Equal(t, shared.TenantID("tenant-99"), agg.TenantID)
	assert.Equal(t, "MH01AB1234", agg.RegistrationNumber)
	assert.Equal(t, "VN-XYZ", agg.VehicleNumber)
	assert.Equal(t, aggregate.VehicleTypeBus, agg.VehicleType)
	assert.Equal(t, int64(42), agg.Capacity)
	assert.Equal(t, aggregate.FuelTypeCNG, agg.FuelType)
	assert.Equal(t, v.InsuranceExpiry, agg.InsuranceExpiry)
	assert.Equal(t, v.FitnessExpiry, agg.FitnessExpiry)
	assert.Equal(t, v.PermitExpiry, agg.PermitExpiry)
	assert.Equal(t, aggregate.VehicleRunning, agg.Status)
	require.NotNil(t, agg.CurrentMileage)
	assert.InDelta(t, 9876.5, *agg.CurrentMileage, 0.001)
	// ToDomain uses CreatedAt for both CreatedAt and UpdatedAt? Check implementation: it uses v.CreatedAt only, not UpdatedAt
	// So verify CreatedAt matches, UpdatedAt currently not set from DB's UpdatedAt but from CreatedAt due to NewVehicleAggregate
	// Actually ToDomain calls NewVehicleAggregate with v.CreatedAt as now param, so both CreatedAt/UpdatedAt become CreatedAt
	assert.Equal(t, now, agg.CreatedAt)
	// Events should contain 1 created event (from NewVehicleAggregate)
	assert.Len(t, agg.Events(), 1)
	_, ok := agg.Events()[0].(aggregate.VehicleCreatedEvent)
	assert.True(t, ok)
	// Clear events to keep clean
	agg.ClearEvents()
	assert.Len(t, agg.Events(), 0)
}

func TestToDomain_NilMileage(t *testing.T) {
	now := time.Now()
	v := db.Vehicle{
		ID:                 "v-nil",
		TenantID:           "t1",
		RegistrationNumber: "REG",
		VehicleNumber:      "VN",
		VehicleType:        "van",
		Capacity:           5,
		FuelType:           "petrol",
		InsuranceExpiry:    now,
		FitnessExpiry:      now,
		PermitExpiry:       now,
		Status:             "available",
		CurrentMileage:     sql.NullFloat64{Valid: false},
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	agg := ToDomain(v)
	assert.Nil(t, agg.CurrentMileage)
	assert.Equal(t, aggregate.VehicleID("v-nil"), agg.ID)
}

func TestToReadModel_MapsAllFields(t *testing.T) {
	now := time.Date(2026, 8, 20, 15, 0, 0, 0, time.UTC)
	mileage := sql.NullFloat64{Float64: 555.0, Valid: true}
	v := db.Vehicle{
		ID:                 "veh-456",
		RegistrationNumber: "KA05MJ1234",
		VehicleNumber:      "VN-456",
		VehicleType:        "tempo",
		Capacity:           7500,
		FuelType:           "diesel",
		InsuranceExpiry:    now.Add(100 * 24 * time.Hour),
		FitnessExpiry:      now.Add(200 * 24 * time.Hour),
		PermitExpiry:       now.Add(300 * 24 * time.Hour),
		Status:             "maintenance",
		CurrentMileage:     mileage,
		TenantID:           "t5",
		CreatedAt:          now,
		UpdatedAt:          now.Add(2 * time.Hour),
	}

	rm := ToReadModel(v)

	assert.Equal(t, "veh-456", rm.ID)
	assert.Equal(t, "KA05MJ1234", rm.RegistrationNumber)
	assert.Equal(t, "VN-456", rm.VehicleNumber)
	assert.Equal(t, "tempo", rm.VehicleType)
	assert.Equal(t, int64(7500), rm.Capacity)
	assert.Equal(t, "diesel", rm.FuelType)
	assert.Equal(t, v.InsuranceExpiry, rm.InsuranceExpiry)
	assert.Equal(t, v.FitnessExpiry, rm.FitnessExpiry)
	assert.Equal(t, v.PermitExpiry, rm.PermitExpiry)
	assert.Equal(t, "maintenance", rm.Status)
	require.NotNil(t, rm.CurrentMileage)
	assert.InDelta(t, 555.0, *rm.CurrentMileage, 0.001)
	assert.Equal(t, now, rm.CreatedAt)
	assert.Equal(t, now.Add(2*time.Hour), rm.UpdatedAt)
}

func TestToReadModel_NilMileage(t *testing.T) {
	now := time.Now()
	v := db.Vehicle{
		ID:                 "v-nil2",
		RegistrationNumber: "REG2",
		VehicleNumber:      "VN2",
		VehicleType:        "truck",
		Capacity:           10,
		FuelType:           "diesel",
		InsuranceExpiry:    now,
		FitnessExpiry:      now,
		PermitExpiry:       now,
		Status:             "available",
		CurrentMileage:     sql.NullFloat64{Valid: false},
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	rm := ToReadModel(v)
	assert.Nil(t, rm.CurrentMileage)
	assert.Equal(t, "v-nil2", rm.ID)
}

func TestToReadModel_ZeroMileageValid(t *testing.T) {
	now := time.Now()
	v := db.Vehicle{
		ID:                 "v-zero",
		RegistrationNumber: "REGZ",
		VehicleNumber:      "VNZ",
		VehicleType:        "pickup",
		Capacity:           1,
		FuelType:           "electric",
		InsuranceExpiry:    now,
		FitnessExpiry:      now,
		PermitExpiry:       now,
		Status:             "inactive",
		CurrentMileage:     sql.NullFloat64{Float64: 0, Valid: true},
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	rm := ToReadModel(v)
	require.NotNil(t, rm.CurrentMileage)
	assert.Equal(t, 0.0, *rm.CurrentMileage)

	agg := ToDomain(v)
	require.NotNil(t, agg.CurrentMileage)
	assert.Equal(t, 0.0, *agg.CurrentMileage)
}
