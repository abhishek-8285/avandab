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
		InsuranceExpiry:    sql.NullTime{Time: now, Valid: true},
		FitnessExpiry:      sql.NullTime{Time: now, Valid: true},
		PermitExpiry:       sql.NullTime{Time: now, Valid: true},
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
		InsuranceExpiry:    sql.NullTime{Time: now.Add(10 * 24 * time.Hour), Valid: true},
		FitnessExpiry:      sql.NullTime{Time: now.Add(20 * 24 * time.Hour), Valid: true},
		PermitExpiry:       sql.NullTime{Time: now.Add(30 * 24 * time.Hour), Valid: true},
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
	assert.Equal(t, v.InsuranceExpiry.Time, agg.InsuranceExpiry)
	assert.Equal(t, v.FitnessExpiry.Time, agg.FitnessExpiry)
	assert.Equal(t, v.PermitExpiry.Time, agg.PermitExpiry)
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
		InsuranceExpiry:    sql.NullTime{Time: now, Valid: true},
		FitnessExpiry:      sql.NullTime{Time: now, Valid: true},
		PermitExpiry:       sql.NullTime{Time: now, Valid: true},
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
		InsuranceExpiry:    sql.NullTime{Time: now.Add(100 * 24 * time.Hour), Valid: true},
		FitnessExpiry:      sql.NullTime{Time: now.Add(200 * 24 * time.Hour), Valid: true},
		PermitExpiry:       sql.NullTime{Time: now.Add(300 * 24 * time.Hour), Valid: true},
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
	assert.Equal(t, v.InsuranceExpiry.Time, rm.InsuranceExpiry)
	assert.Equal(t, v.FitnessExpiry.Time, rm.FitnessExpiry)
	assert.Equal(t, v.PermitExpiry.Time, rm.PermitExpiry)
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
		InsuranceExpiry:    sql.NullTime{Time: now, Valid: true},
		FitnessExpiry:      sql.NullTime{Time: now, Valid: true},
		PermitExpiry:       sql.NullTime{Time: now, Valid: true},
		Status:             "available",
		CurrentMileage:     sql.NullFloat64{Valid: false},
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	rm := ToReadModel(v)
	assert.Nil(t, rm.CurrentMileage)
	assert.Equal(t, "v-nil2", rm.ID)
}

func TestToDomain_MapsComplianceFields(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	rc := now.Add(30 * 24 * time.Hour)
	puc := now.Add(60 * 24 * time.Hour)
	v := db.Vehicle{
		ID:                 "veh-c",
		TenantID:           "t1",
		RegistrationNumber: "MH01AB1234",
		VehicleNumber:      "VN-C",
		VehicleType:        string(aggregate.VehicleTypeTruck),
		Capacity:           10,
		FuelType:           string(aggregate.FuelTypeDiesel),
		InsuranceExpiry:    sql.NullTime{Time: now, Valid: true},
		FitnessExpiry:      sql.NullTime{Time: now, Valid: true},
		PermitExpiry:       sql.NullTime{Time: now, Valid: true},
		Status:             string(aggregate.VehicleAvailable),
		CurrentMileage:     sql.NullFloat64{Valid: false},
		Blocked:            1,
		BlockedReason:      sql.NullString{String: "RC expired", Valid: true},
		RcExpiry:           sql.NullTime{Time: rc, Valid: true},
		Odometer:           12345.5,
		PucExpiry:          sql.NullTime{Time: puc, Valid: true},
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	agg := ToDomain(v)
	assert.True(t, agg.Blocked)
	assert.Equal(t, "RC expired", agg.BlockedReason)
	require.NotNil(t, agg.RCExpiry)
	assert.Equal(t, rc, *agg.RCExpiry)
	require.NotNil(t, agg.PUCExpiry)
	assert.Equal(t, puc, *agg.PUCExpiry)
	assert.InDelta(t, 12345.5, agg.Odometer, 0.001)

	rm := ToReadModel(v)
	assert.True(t, rm.Blocked)
	assert.Equal(t, "RC expired", rm.BlockedReason)
	require.NotNil(t, rm.RCExpiry)
	assert.Equal(t, rc, *rm.RCExpiry)
	require.NotNil(t, rm.PUCExpiry)
	assert.Equal(t, puc, *rm.PUCExpiry)
	assert.InDelta(t, 12345.5, rm.Odometer, 0.001)

	// Unset compliance reads as zero values (no block).
	v.Blocked = 0
	v.BlockedReason = sql.NullString{}
	v.RcExpiry = sql.NullTime{}
	v.PucExpiry = sql.NullTime{}
	v.Odometer = 0
	agg = ToDomain(v)
	assert.False(t, agg.Blocked)
	assert.Empty(t, agg.BlockedReason)
	assert.Nil(t, agg.RCExpiry)
	assert.Nil(t, agg.PUCExpiry)
	assert.NoError(t, agg.CanAssign(now))

	// Unset doc dates (00132 side-effect registrations) read as zero values.
	v.InsuranceExpiry = sql.NullTime{}
	v.FitnessExpiry = sql.NullTime{}
	v.PermitExpiry = sql.NullTime{}
	agg = ToDomain(v)
	assert.True(t, agg.InsuranceExpiry.IsZero())
	assert.True(t, agg.FitnessExpiry.IsZero())
	assert.True(t, agg.PermitExpiry.IsZero())
	assert.NoError(t, agg.CanAssign(now))
	rm = ToReadModel(v)
	assert.True(t, rm.InsuranceExpiry.IsZero())
	assert.True(t, rm.FitnessExpiry.IsZero())
	assert.True(t, rm.PermitExpiry.IsZero())
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
		InsuranceExpiry:    sql.NullTime{Time: now, Valid: true},
		FitnessExpiry:      sql.NullTime{Time: now, Valid: true},
		PermitExpiry:       sql.NullTime{Time: now, Valid: true},
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
