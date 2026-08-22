package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/shared"
	"transport-app/internal/shared/ports"
	"transport-app/internal/vehicle/domain"
	"transport-app/internal/vehicle/domain/aggregate"
)

// ---- mocks ----

type mockIDGen struct {
	id string
}

func (m *mockIDGen) GenerateUUID() string                   { return m.id }
func (m *mockIDGen) GenerateDisplayID(prefix string) string { return prefix + "-123" }

type mockClock struct {
	now time.Time
}

func (m *mockClock) Now() time.Time { return m.now }

type mockVehicleRepo struct {
	saveErr            error
	saved              []*aggregate.VehicleAggregate
	findErr            error
	findResult         *aggregate.VehicleAggregate
	getReadModelErr    error
	getReadModelResult domain.VehicleReadModel
	searchErr          error
	searchResult       []domain.VehicleReadModel
	searchTotal        int64
	capturedLimit      int
	capturedOffset     int
}

func (m *mockVehicleRepo) Save(ctx context.Context, v *aggregate.VehicleAggregate) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.saved = append(m.saved, v)
	return nil
}
func (m *mockVehicleRepo) Find(ctx context.Context, id aggregate.VehicleID, tenantID shared.TenantID) (*aggregate.VehicleAggregate, error) {
	if m.findErr != nil {
		return nil, m.findErr
	}
	if m.findResult != nil {
		return m.findResult, nil
	}
	return nil, errors.New("not found")
}
func (m *mockVehicleRepo) GetReadModel(ctx context.Context, id aggregate.VehicleID, tenantID shared.TenantID) (domain.VehicleReadModel, error) {
	if m.getReadModelErr != nil {
		return domain.VehicleReadModel{}, m.getReadModelErr
	}
	return m.getReadModelResult, nil
}
func (m *mockVehicleRepo) SearchReadModels(ctx context.Context, tenantID shared.TenantID, query string, status string, limit int, offset int) ([]domain.VehicleReadModel, int64, error) {
	m.capturedLimit = limit
	m.capturedOffset = offset
	if m.searchErr != nil {
		return nil, 0, m.searchErr
	}
	return m.searchResult, m.searchTotal, nil
}

type mockRepoProvider struct {
	vehicles any
}

func (m *mockRepoProvider) Bookings() any    { return nil }
func (m *mockRepoProvider) Trips() any       { return nil }
func (m *mockRepoProvider) Drivers() any     { return nil }
func (m *mockRepoProvider) Vehicles() any    { return m.vehicles }
func (m *mockRepoProvider) Invoices() any    { return nil }
func (m *mockRepoProvider) Payments() any    { return nil }
func (m *mockRepoProvider) AuditLogs() any   { return nil }
func (m *mockRepoProvider) Maintenance() any { return nil }

type mockTxContext struct {
	context.Context
	provider ports.RepositoryProvider
}

func (m *mockTxContext) Repositories() ports.RepositoryProvider { return m.provider }

type mockUoW struct {
	provider any
	execErr  error
	called   bool
}

func (m *mockUoW) Execute(ctx context.Context, fn func(txCtx ports.TxContext) error) error {
	m.called = true
	if m.execErr != nil {
		return m.execErr
	}
	provider := &mockRepoProvider{vehicles: m.provider}
	txCtx := &mockTxContext{Context: ctx, provider: provider}
	return fn(txCtx)
}

// ---- tests for CreateVehicleUseCase ----

func TestCreateVehicleUseCase_ValidationError(t *testing.T) {
	clk := &mockClock{now: time.Now()}
	idGen := &mockIDGen{id: "veh-123"}
	repo := &mockVehicleRepo{}
	uow := &mockUoW{provider: repo}

	uc := NewCreateVehicleUseCase(uow, idGen, clk)

	tests := []struct {
		name string
		cmd  CreateVehicleCommand
	}{
		{"empty registration", CreateVehicleCommand{TenantID: "t1", RegistrationNumber: "", VehicleNumber: "VN1"}},
		{"empty vehicle number", CreateVehicleCommand{TenantID: "t1", RegistrationNumber: "REG1", VehicleNumber: ""}},
		{"both empty", CreateVehicleCommand{TenantID: "t1", RegistrationNumber: "", VehicleNumber: ""}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := uc.Execute(context.Background(), tc.cmd)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "registration number and vehicle number are required")
			assert.False(t, uow.called, "UoW should not be called on validation error")
			uow.called = false
		})
	}
}

func TestCreateVehicleUseCase_Success(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	clk := &mockClock{now: now}
	idGen := &mockIDGen{id: "veh-generated-id"}
	repo := &mockVehicleRepo{}
	uow := &mockUoW{provider: repo}

	uc := NewCreateVehicleUseCase(uow, idGen, clk)

	mileage := 555.5
	cmd := CreateVehicleCommand{
		TenantID:           "tenant-1",
		RegistrationNumber: "MH01AB1234",
		VehicleNumber:      "VN-001",
		VehicleType:        aggregate.VehicleTypeTruck,
		Capacity:           10000,
		FuelType:           aggregate.FuelTypeDiesel,
		InsuranceExpiry:    now.Add(365 * 24 * time.Hour),
		FitnessExpiry:      now.Add(365 * 24 * time.Hour),
		PermitExpiry:       now.Add(365 * 24 * time.Hour),
		CurrentMileage:     &mileage,
	}
	id, err := uc.Execute(context.Background(), cmd)
	require.NoError(t, err)
	assert.Equal(t, aggregate.VehicleID("veh-generated-id"), id)
	require.Len(t, repo.saved, 1)
	saved := repo.saved[0]
	assert.Equal(t, aggregate.VehicleID("veh-generated-id"), saved.ID)
	assert.Equal(t, shared.TenantID("tenant-1"), saved.TenantID)
	assert.Equal(t, "MH01AB1234", saved.RegistrationNumber)
	assert.Equal(t, "VN-001", saved.VehicleNumber)
	assert.Equal(t, aggregate.VehicleTypeTruck, saved.VehicleType)
	assert.Equal(t, int64(10000), saved.Capacity)
	assert.Equal(t, aggregate.FuelTypeDiesel, saved.FuelType)
	assert.Equal(t, aggregate.VehicleAvailable, saved.Status)
	require.NotNil(t, saved.CurrentMileage)
	assert.InDelta(t, 555.5, *saved.CurrentMileage, 0.001)
	assert.Equal(t, now, saved.CreatedAt)
	assert.Equal(t, now, saved.UpdatedAt)
	// Should have created event
	assert.Len(t, saved.Events(), 1)
}

func TestCreateVehicleUseCase_SuccessNilMileage(t *testing.T) {
	now := time.Now()
	clk := &mockClock{now: now}
	idGen := &mockIDGen{id: "veh-1"}
	repo := &mockVehicleRepo{}
	uow := &mockUoW{provider: repo}
	uc := NewCreateVehicleUseCase(uow, idGen, clk)

	cmd := CreateVehicleCommand{
		TenantID:           "t1",
		RegistrationNumber: "REG1",
		VehicleNumber:      "VN1",
		VehicleType:        aggregate.VehicleTypeBus,
		Capacity:           40,
		FuelType:           aggregate.FuelTypeCNG,
		InsuranceExpiry:    now,
		FitnessExpiry:      now,
		PermitExpiry:       now,
		CurrentMileage:     nil,
	}
	id, err := uc.Execute(context.Background(), cmd)
	require.NoError(t, err)
	assert.Equal(t, aggregate.VehicleID("veh-1"), id)
	require.Len(t, repo.saved, 1)
	assert.Nil(t, repo.saved[0].CurrentMileage)
}

func TestCreateVehicleUseCase_RepoTypeAssertionFailure(t *testing.T) {
	clk := &mockClock{now: time.Now()}
	idGen := &mockIDGen{id: "veh-1"}
	uow := &mockUoW{provider: "not-a-repo"} // wrong type
	uc := NewCreateVehicleUseCase(uow, idGen, clk)

	cmd := CreateVehicleCommand{
		TenantID:           "t1",
		RegistrationNumber: "REG1",
		VehicleNumber:      "VN1",
		VehicleType:        aggregate.VehicleTypeTruck,
		Capacity:           10,
		FuelType:           aggregate.FuelTypeDiesel,
		InsuranceExpiry:    time.Now(),
		FitnessExpiry:      time.Now(),
		PermitExpiry:       time.Now(),
	}
	_, err := uc.Execute(context.Background(), cmd)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to retrieve vehicle repository")
}

func TestCreateVehicleUseCase_SaveError(t *testing.T) {
	clk := &mockClock{now: time.Now()}
	idGen := &mockIDGen{id: "veh-1"}
	repo := &mockVehicleRepo{saveErr: errors.New("db save failed")}
	uow := &mockUoW{provider: repo}
	uc := NewCreateVehicleUseCase(uow, idGen, clk)

	cmd := CreateVehicleCommand{
		TenantID:           "t1",
		RegistrationNumber: "REG1",
		VehicleNumber:      "VN1",
		VehicleType:        aggregate.VehicleTypeTruck,
		Capacity:           10,
		FuelType:           aggregate.FuelTypeDiesel,
		InsuranceExpiry:    time.Now(),
		FitnessExpiry:      time.Now(),
		PermitExpiry:       time.Now(),
	}
	_, err := uc.Execute(context.Background(), cmd)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db save failed")
}

func TestCreateVehicleUseCase_UoWError(t *testing.T) {
	clk := &mockClock{now: time.Now()}
	idGen := &mockIDGen{id: "veh-1"}
	uow := &mockUoW{execErr: errors.New("uow failed")}
	uc := NewCreateVehicleUseCase(uow, idGen, clk)

	cmd := CreateVehicleCommand{
		TenantID:           "t1",
		RegistrationNumber: "REG1",
		VehicleNumber:      "VN1",
		VehicleType:        aggregate.VehicleTypeTruck,
		Capacity:           10,
		FuelType:           aggregate.FuelTypeDiesel,
		InsuranceExpiry:    time.Now(),
		FitnessExpiry:      time.Now(),
		PermitExpiry:       time.Now(),
	}
	_, err := uc.Execute(context.Background(), cmd)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "uow failed")
}
