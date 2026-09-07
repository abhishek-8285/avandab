package application

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/shared"
	"transport-app/internal/vehicle/domain"
	"transport-app/internal/vehicle/domain/aggregate"
	sqlrepo "transport-app/internal/vehicle/infrastructure/persistence/sql"
)

// ---- measurement fake ----

type fakeMeasurementRepo struct {
	mockVehicleRepo
	points       map[string]domain.MeasuringPoint
	measurements map[string][]domain.Measurement
	recordErr    error
}

func newFakeMeasurementRepo() *fakeMeasurementRepo {
	return &fakeMeasurementRepo{
		points:       map[string]domain.MeasuringPoint{},
		measurements: map[string][]domain.Measurement{},
	}
}

func (f *fakeMeasurementRepo) CreateMeasuringPoint(ctx context.Context, tenantID shared.TenantID, p domain.MeasuringPoint) (domain.MeasuringPoint, error) {
	f.points[p.ID] = p
	return p, nil
}

func (f *fakeMeasurementRepo) ListMeasuringPoints(ctx context.Context, tenantID shared.TenantID, vehicleID string) ([]domain.MeasuringPoint, error) {
	var out []domain.MeasuringPoint
	for _, p := range f.points {
		if p.VehicleID == vehicleID {
			out = append(out, p)
		}
	}
	return out, nil
}

func (f *fakeMeasurementRepo) RecordMeasurement(ctx context.Context, tenantID shared.TenantID, m domain.Measurement) (domain.Measurement, error) {
	if f.recordErr != nil {
		return domain.Measurement{}, f.recordErr
	}
	f.measurements[m.PointID] = append(f.measurements[m.PointID], m)
	return m, nil
}

func (f *fakeMeasurementRepo) ListMeasurements(ctx context.Context, tenantID shared.TenantID, pointID string, limit int, offset int) ([]domain.Measurement, error) {
	return f.measurements[pointID], nil
}

func (f *fakeMeasurementRepo) LastMeasurement(ctx context.Context, tenantID shared.TenantID, pointID string) (domain.Measurement, error) {
	ms := f.measurements[pointID]
	if len(ms) == 0 {
		return domain.Measurement{}, sqlrepo.ErrNoMeasurement
	}
	return ms[len(ms)-1], nil
}

// ---- tests ----

func TestRecordMeasurement_FirstDocDifferenceEqualsCounter(t *testing.T) {
	repo := newFakeMeasurementRepo()
	uow := &mockUoW{provider: repo}
	uc := NewRecordMeasurementUseCase(uow, &mockIDGen{id: "m1"}, &mockClock{now: time.Now()})

	out, err := uc.Execute(context.Background(), RecordMeasurementCommand{
		TenantID: shared.TenantID("t1"), PointID: "p1", Counter: 1200, ReadBy: "TCS795488",
	})
	require.NoError(t, err)
	assert.Equal(t, 1200.0, out.CounterReading)
	assert.Equal(t, 1200.0, out.DifferenceReading, "first IK11: difference = counter itself (TMS_SOP p.6)")
	assert.Equal(t, 1200.0, out.TotalCounterReading)
}

func TestRecordMeasurement_MonotonicReject(t *testing.T) {
	repo := newFakeMeasurementRepo()
	repo.measurements["p1"] = []domain.Measurement{{ID: "m0", PointID: "p1", CounterReading: 1500}}
	uow := &mockUoW{provider: repo}
	uc := NewRecordMeasurementUseCase(uow, &mockIDGen{id: "m1"}, &mockClock{now: time.Now()})

	_, err := uc.Execute(context.Background(), RecordMeasurementCommand{
		TenantID: shared.TenantID("t1"), PointID: "p1", Counter: 1200,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "backwards")
}

func TestRecordMeasurement_Validation(t *testing.T) {
	repo := newFakeMeasurementRepo()
	uow := &mockUoW{provider: repo}
	uc := NewRecordMeasurementUseCase(uow, &mockIDGen{id: "m1"}, &mockClock{now: time.Now()})

	_, err := uc.Execute(context.Background(), RecordMeasurementCommand{TenantID: "t1"})
	require.Error(t, err, "point id required")

	_, err = uc.Execute(context.Background(), RecordMeasurementCommand{TenantID: "t1", PointID: "p1", Counter: -5})
	require.Error(t, err, "negative counter rejected")
}

func TestRecordMeasurement_RepoMissing(t *testing.T) {
	uow := &mockUoW{provider: &mockVehicleRepo{}}
	uc := NewRecordMeasurementUseCase(uow, &mockIDGen{id: "m1"}, &mockClock{now: time.Now()})

	_, err := uc.Execute(context.Background(), RecordMeasurementCommand{TenantID: "t1", PointID: "p1", Counter: 10})
	require.Error(t, err)
}

func TestCreateVehicleUseCase_WithProfile(t *testing.T) {
	repo := &mockVehicleRepo{}
	uow := &mockUoW{provider: repo}
	uc := NewCreateVehicleUseCase(uow, &mockIDGen{id: "v9"}, &mockClock{now: time.Now()})

	now := time.Now()
	_, err := uc.Execute(context.Background(), CreateVehicleCommand{
		TenantID:           shared.TenantID("t1"),
		RegistrationNumber: "KA30P1234",
		VehicleNumber:      "VN-9",
		VehicleType:        aggregate.VehicleTypeTruck,
		FuelType:           aggregate.FuelTypeDiesel,
		InsuranceExpiry:    now, FitnessExpiry: now, PermitExpiry: now,
		Profile: aggregate.VehicleProfile{
			FleetClass: aggregate.FleetClassCV, Ownership: aggregate.OwnershipOwn,
			Manufacturer: "TATA", FacilityID: "MM21000000757",
		},
	})
	require.NoError(t, err)
	require.Len(t, repo.saved, 1)
	assert.Equal(t, "TATA", repo.saved[0].Profile.Manufacturer)
	assert.Equal(t, aggregate.FleetClassCV, repo.saved[0].Profile.FleetClass)
}

func TestCreateVehicleUseCase_InvalidProfile(t *testing.T) {
	repo := &mockVehicleRepo{}
	uow := &mockUoW{provider: repo}
	uc := NewCreateVehicleUseCase(uow, &mockIDGen{id: "v9"}, &mockClock{now: time.Now()})

	now := time.Now()
	_, err := uc.Execute(context.Background(), CreateVehicleCommand{
		TenantID:           shared.TenantID("t1"),
		RegistrationNumber: "KA30P1234",
		VehicleNumber:      "VN-9",
		VehicleType:        aggregate.VehicleTypeTruck,
		FuelType:           aggregate.FuelTypeDiesel,
		InsuranceExpiry:    now, FitnessExpiry: now, PermitExpiry: now,
		Profile: aggregate.VehicleProfile{FleetClass: "XX"},
	})
	require.Error(t, err)
	assert.Empty(t, repo.saved)
}
