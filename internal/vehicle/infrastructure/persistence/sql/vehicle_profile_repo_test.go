package sql

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/shared"
	"transport-app/internal/vehicle/domain"
	"transport-app/internal/vehicle/domain/aggregate"
)

func testProfile(now time.Time) aggregate.VehicleProfile {
	acq := now.Add(-24 * time.Hour)
	validFrom := now
	return aggregate.VehicleProfile{
		FleetClass: aggregate.FleetClassCV, Ownership: aggregate.OwnershipOwn,
		FleetNumber: "35", Description: "TATA TRUCK", Manufacturer: "TATA",
		ManufCountry: "IN", Model: "TRUCK", ConstrYearMonth: "2015 / 07",
		AcquisitionCurrency: "INR", AcquisitionDate: &acq, PurchaseVendor: "Vendor",
		ValidFrom: &validFrom, FacilityID: "MM21000000757", MaintPlant: "KMM1",
		PlanningPlant: "KMM1", CompanyCode: "DOPI", BusinessArea: "1013",
		CostCenter: "2111000000", FleetObjectNo: "BLR1R0CKTMR", ChassisNo: "CHS1",
		VehicleCategory: "1", EngineNumber: "ENG1", SecondaryFuel: "",
		UsageIndicator: "M", WeightUnit: "TO",
	}
}

func TestVehicleRepository_Save_ProfileRoundTrip(t *testing.T) {
	dbConn := setupVehicleTestDB(t)
	repo := NewVehicleRepository(dbConn).(*vehicleRepository)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	agg := newTestVehicleAgg("veh-p", "t1", "KA30P1234", "VN-P", aggregate.VehicleTypeTruck, 10, aggregate.FuelTypeDiesel, aggregate.VehicleAvailable, nil, now)
	require.NoError(t, agg.ApplyProfile(testProfile(now), now))
	require.NoError(t, repo.Save(ctx, agg))

	found, err := repo.Find(ctx, "veh-p", "t1")
	require.NoError(t, err)
	assert.Equal(t, aggregate.FleetClassCV, found.Profile.FleetClass)
	assert.Equal(t, aggregate.OwnershipOwn, found.Profile.Ownership)
	assert.Equal(t, "35", found.Profile.FleetNumber)
	assert.Equal(t, "TATA", found.Profile.Manufacturer)
	assert.Equal(t, "MM21000000757", found.Profile.FacilityID)
	assert.Equal(t, "BLR1R0CKTMR", found.Profile.FleetObjectNo)
	assert.Equal(t, "M", found.Profile.UsageIndicator)
	assert.Equal(t, "INR", found.Profile.AcquisitionCurrency)

	rm, err := repo.GetReadModel(ctx, "veh-p", "t1")
	require.NoError(t, err)
	assert.Equal(t, "TATA", rm.Profile.Manufacturer)
	assert.Equal(t, "35", rm.Profile.FleetNumber)
}

func TestVehicleRepository_Search_FleetClassFilter(t *testing.T) {
	dbConn := setupVehicleTestDB(t)
	repo := NewVehicleRepository(dbConn).(*vehicleRepository)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	mk := func(id, reg, fc string) {
		agg := newTestVehicleAgg(id, "t1", reg, "VN-"+id, aggregate.VehicleTypeTruck, 10, aggregate.FuelTypeDiesel, aggregate.VehicleAvailable, nil, now)
		require.NoError(t, agg.ApplyProfile(aggregate.VehicleProfile{FleetClass: aggregate.FleetClass(fc)}, now))
		require.NoError(t, repo.Save(ctx, agg))
	}
	mk("v-cv", "REG-CV", "CV")
	mk("v-fs", "REG-FS", "FS")

	rows, total, err := repo.SearchReadModelsFiltered(ctx, shared.TenantID("t1"), "", "", "FS", "", "", "", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, rows, 1)
	assert.Equal(t, "REG-FS", rows[0].RegistrationNumber)
	assert.Equal(t, "FS", string(rows[0].Profile.FleetClass))

	// Legacy unfiltered path still returns both.
	rows, total, err = repo.SearchReadModels(ctx, shared.TenantID("t1"), "", "", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	assert.Len(t, rows, 2)
}

func TestVehicleRepository_Measurements_RoundTrip(t *testing.T) {
	dbConn := setupVehicleTestDB(t)
	repo := NewVehicleRepository(dbConn).(*vehicleRepository)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	agg := newTestVehicleAgg("veh-m", "t1", "KA01KA0123", "VN-M", aggregate.VehicleTypeTruck, 10, aggregate.FuelTypeDiesel, aggregate.VehicleAvailable, nil, now)
	require.NoError(t, repo.Save(ctx, agg))

	pt, err := repo.CreateMeasuringPoint(ctx, shared.TenantID("t1"), domain.MeasuringPoint{
		ID: "pt-782", VehicleID: "veh-m", Category: "M", Kind: "ODO",
		MeasPosition: "DISTANCE", Unit: "KM", AnnualEstimate: 50000,
		IsCounter: true, Description: "Tata Truck KA01KA0123",
	})
	require.NoError(t, err)
	assert.Equal(t, "pt-782", pt.ID)

	_, err = repo.LastMeasurement(ctx, shared.TenantID("t1"), "pt-782")
	require.ErrorIs(t, err, ErrNoMeasurement)

	m1, err := repo.RecordMeasurement(ctx, shared.TenantID("t1"), domain.Measurement{
		ID: "m-1197", PointID: "pt-782", CounterReading: 1200,
		DifferenceReading: 1200, TotalCounterReading: 1200,
		MeasuredAt: now, ReadBy: "TCS795488",
	})
	require.NoError(t, err)
	assert.Equal(t, 1200.0, m1.CounterReading)
	assert.Equal(t, 1200.0, m1.DifferenceReading)

	last, err := repo.LastMeasurement(ctx, shared.TenantID("t1"), "pt-782")
	require.NoError(t, err)
	assert.Equal(t, "m-1197", last.ID)

	pts, err := repo.ListMeasuringPoints(ctx, shared.TenantID("t1"), "veh-m")
	require.NoError(t, err)
	require.Len(t, pts, 1)
	assert.Equal(t, 50000.0, pts[0].AnnualEstimate)
}
