package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"transport-app/internal/fuel"
	"transport-app/internal/fuel/application"
)

type mockFuelCardRepo struct {
	registerCardFn              func(ctx context.Context, card fuel.FuelCard) (*fuel.FuelCard, error)
	verifyAssignmentOwnershipFn func(ctx context.Context, tenantID string, vehicleID, driverID *string) error
	listCardsFn                 func(ctx context.Context, tenantID string) ([]fuel.FuelCard, error)
}

func (m *mockFuelCardRepo) RegisterCard(ctx context.Context, card fuel.FuelCard) (*fuel.FuelCard, error) {
	if m.registerCardFn != nil {
		return m.registerCardFn(ctx, card)
	}
	return &card, nil
}
func (m *mockFuelCardRepo) GetCardByTokenHash(ctx context.Context, tenantID, tokenHash string) (*fuel.FuelCard, error) {
	return nil, nil
}
func (m *mockFuelCardRepo) GetCardByID(ctx context.Context, tenantID, cardID string) (*fuel.FuelCard, error) {
	return nil, nil
}
func (m *mockFuelCardRepo) ListCards(ctx context.Context, tenantID string) ([]fuel.FuelCard, error) {
	if m.listCardsFn != nil {
		return m.listCardsFn(ctx, tenantID)
	}
	return nil, nil
}
func (m *mockFuelCardRepo) GetVehicleTankCapacity(ctx context.Context, tenantID, vehicleID string) (float64, error) {
	return 200, nil
}
func (m *mockFuelCardRepo) FindMatchingExpense(ctx context.Context, tenantID string, vehicleID, driverID *string, amount float64, txnTime time.Time) (string, error) {
	return "", nil
}
func (m *mockFuelCardRepo) CreateVerifiedExpense(ctx context.Context, tenantID string, vehicleID, driverID *string, amount, litres float64, txnTime time.Time, notes string) (string, error) {
	return "exp-1", nil
}
func (m *mockFuelCardRepo) MarkExpenseVerified(ctx context.Context, tenantID, expenseID, reason string) error {
	return nil
}
func (m *mockFuelCardRepo) PostGeneralLedgerAndSyncLog(ctx context.Context, tenantID string, txn fuel.FuelCardTransaction, provider string) (string, error) {
	return "gl-1", nil
}
func (m *mockFuelCardRepo) InsertTransaction(ctx context.Context, txn fuel.FuelCardTransaction) (*fuel.FuelCardTransaction, error) {
	return &txn, nil
}
func (m *mockFuelCardRepo) GetTransactionByID(ctx context.Context, tenantID, txnID string) (*fuel.FuelCardTransaction, error) {
	return nil, nil
}
func (m *mockFuelCardRepo) GetTransactionByExternalID(ctx context.Context, tenantID, externalTxnID string) (*fuel.FuelCardTransaction, error) {
	return nil, nil
}
func (m *mockFuelCardRepo) ReconcileTransaction(ctx context.Context, tenantID, txnID, expenseID, notes string) (*fuel.FuelCardTransaction, error) {
	return nil, nil
}
func (m *mockFuelCardRepo) RecordPilferageAlert(ctx context.Context, tenantID, vehicleID, title, description string) error {
	return nil
}
func (m *mockFuelCardRepo) VerifyAssignmentOwnership(ctx context.Context, tenantID string, vehicleID, driverID *string) error {
	if m.verifyAssignmentOwnershipFn != nil {
		return m.verifyAssignmentOwnershipFn(ctx, tenantID, vehicleID, driverID)
	}
	return nil
}
func (m *mockFuelCardRepo) WithTransaction(ctx context.Context, fn func(fuel.FuelCardRepository) error) error {
	return fn(m)
}

func TestFuelCardUseCase_RegisterCard_Validation(t *testing.T) {
	ctx := context.Background()
	repo := &mockFuelCardRepo{}
	uc := application.NewFuelCardUseCase(repo)

	// 1. Missing card number
	_, err := uc.RegisterCard(ctx, "t-1", fuel.RegisterFuelCardRequest{
		CardNumber: "",
		Provider:   "IOCL",
	})
	if err == nil || err.Error() != "card_number is required" {
		t.Fatalf("expected 'card_number is required', got: %v", err)
	}

	// 2. Invalid provider
	_, err = uc.RegisterCard(ctx, "t-1", fuel.RegisterFuelCardRequest{
		CardNumber: "1234567890123456",
		Provider:   "UNKNOWN_OMC",
	})
	if err == nil {
		t.Fatalf("expected error for invalid provider")
	}

	// 3. Ownership verification failure
	repo.verifyAssignmentOwnershipFn = func(ctx context.Context, tenantID string, vehicleID, driverID *string) error {
		return errors.New("vehicle does not belong to tenant")
	}
	_, err = uc.RegisterCard(ctx, "t-1", fuel.RegisterFuelCardRequest{
		CardNumber: "1234567890123456",
		Provider:   "IOCL",
	})
	if err == nil || err.Error() != "vehicle does not belong to tenant" {
		t.Fatalf("expected ownership error, got: %v", err)
	}

	// 4. Successful registration with default limit
	repo.verifyAssignmentOwnershipFn = nil
	var capturedCard fuel.FuelCard
	repo.registerCardFn = func(ctx context.Context, card fuel.FuelCard) (*fuel.FuelCard, error) {
		capturedCard = card
		return &card, nil
	}

	registered, err := uc.RegisterCard(ctx, "t-1", fuel.RegisterFuelCardRequest{
		CardNumber:      "1234567890123456",
		Provider:        "iocl",
		DailySpendLimit: 0, // Should default to 50000.0
	})
	if err != nil {
		t.Fatalf("unexpected error registering card: %v", err)
	}
	if registered.DailySpendLimit != 50000.0 {
		t.Errorf("expected default limit 50000.0, got %v", registered.DailySpendLimit)
	}
	if capturedCard.Provider != fuel.ProviderIOCL {
		t.Errorf("expected provider IOCL, got %v", capturedCard.Provider)
	}
}

func TestFuelCardUseCase_ListCards(t *testing.T) {
	ctx := context.Background()
	repo := &mockFuelCardRepo{
		listCardsFn: func(ctx context.Context, tenantID string) ([]fuel.FuelCard, error) {
			return []fuel.FuelCard{
				{ID: "card-1", TenantID: tenantID},
				{ID: "card-2", TenantID: tenantID},
			}, nil
		},
	}
	uc := application.NewFuelCardUseCase(repo)

	cards, err := uc.ListCards(ctx, "tenant-test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cards) != 2 {
		t.Fatalf("expected 2 cards, got %d", len(cards))
	}
}
