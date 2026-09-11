package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"transport-app/internal/fuel"
)

// FuelCardUseCase coordinates fuel card operations, OMC statement ingestion, and audit loops.
type FuelCardUseCase struct {
	repo fuel.FuelCardRepository
}

// NewFuelCardUseCase creates a new FuelCardUseCase.
func NewFuelCardUseCase(repo fuel.FuelCardRepository) *FuelCardUseCase {
	return &FuelCardUseCase{repo: repo}
}

// RegisterCard registers a commercial fleet card and stores its token hash and masked number.
func (uc *FuelCardUseCase) RegisterCard(ctx context.Context, tenantID string, req fuel.RegisterFuelCardRequest) (*fuel.FuelCard, error) {
	cleanNum := strings.TrimSpace(req.CardNumber)
	if cleanNum == "" {
		return nil, errors.New("card_number is required")
	}
	prov := strings.ToUpper(strings.TrimSpace(req.Provider))
	switch prov {
	case string(fuel.ProviderIOCL), string(fuel.ProviderBPCL), string(fuel.ProviderHPCL), string(fuel.ProviderShell), string(fuel.ProviderFleetBank):
	default:
		return nil, fmt.Errorf("invalid provider %q; must be IOCL, BPCL, HPCL, SHELL, or FLEET_BANK", req.Provider)
	}

	limit := req.DailySpendLimit
	if limit <= 0 {
		limit = 50000.0 // Default per spec
	}

	card := fuel.FuelCard{
		TenantID:          tenantID,
		CardNumberMasked:  fuel.MaskCardNumber(cleanNum),
		CardTokenHash:     fuel.HashCardToken(cleanNum),
		Provider:          fuel.FuelCardProvider(prov),
		AssignedVehicleID: req.AssignedVehicleID,
		AssignedDriverID:  req.AssignedDriverID,
		DailySpendLimit:   limit,
		Status:            fuel.CardStatusActive,
	}

	return uc.repo.RegisterCard(ctx, card)
}

// ListCards returns all cards for a tenant with spend metrics.
func (uc *FuelCardUseCase) ListCards(ctx context.Context, tenantID string) ([]fuel.FuelCard, error) {
	return uc.repo.ListCards(ctx, tenantID)
}

// SyncResult captures the outcome of statement batch ingestion.
type SyncResult struct {
	IngestedCount  int                        `json:"ingested_count"`
	MatchedCount   int                        `json:"matched_count"`
	GeneratedCount int                        `json:"generated_count"`
	AnomalyCount   int                        `json:"anomaly_count"`
	Transactions   []fuel.FuelCardTransaction `json:"transactions"`
}

// SyncTransactions processes statement records through pilferage check, kharcha cross-check, and GL sync.
func (uc *FuelCardUseCase) SyncTransactions(ctx context.Context, tenantID string, req fuel.SyncFuelTransactionsRequest) (*SyncResult, error) {
	result := &SyncResult{}

	for _, item := range req.Transactions {
		tokenHash := item.CardTokenHash
		if tokenHash == "" && item.CardNumber != "" {
			tokenHash = fuel.HashCardToken(item.CardNumber)
		}
		if tokenHash == "" {
			continue
		}

		card, err := uc.repo.GetCardByTokenHash(ctx, tenantID, tokenHash)
		if err != nil || card == nil {
			// Skip or reject if card not found in this tenant
			continue
		}

		status := fuel.ReconStatusUnreconciled
		var matchedExpenseID *string
		var anomalyNote *string

		// 1. Pilferage Guard: Check vehicle tank capacity
		if card.AssignedVehicleID != nil && *card.AssignedVehicleID != "" {
			tankCap, err := uc.repo.GetVehicleTankCapacity(ctx, tenantID, *card.AssignedVehicleID)
			if err == nil && tankCap > 0 && item.VolumeLitres > (tankCap*1.05) {
				status = fuel.ReconStatusFlaggedAnomaly
				msg := fmt.Sprintf("Fuel volume (%.1fL) exceeds vehicle tank capacity (%.1fL + 5%% tolerance)", item.VolumeLitres, tankCap)
				anomalyNote = &msg
				result.AnomalyCount++
				_ = uc.repo.RecordPilferageAlert(ctx, tenantID, *card.AssignedVehicleID, "Fuel Volume Pilferage Risk", msg)
			}
		}

		// 2. Kharcha Cross-Check (Spec 20 §3.2)
		if status != fuel.ReconStatusFlaggedAnomaly {
			foundExpID, expErr := uc.repo.FindMatchingExpense(ctx, tenantID, card.AssignedVehicleID, card.AssignedDriverID, item.TotalAmount, item.TxnTime)
			if expErr == nil && foundExpID != "" {
				// Matched existing driver expense claim
				matchedExpenseID = &foundExpID
				status = fuel.ReconStatusMatchedExpense
				_ = uc.repo.MarkExpenseVerified(ctx, tenantID, foundExpID, fmt.Sprintf("Verified via %s fuel card txn %s", card.Provider, item.ExternalTxnID))
				result.MatchedCount++
			} else {
				// Auto-create verified driver expense
				genNotes := fmt.Sprintf("Auto-generated from %s fuel card txn %s at %s", card.Provider, item.ExternalTxnID, item.FuelStationName)
				newExpID, genErr := uc.repo.CreateVerifiedExpense(ctx, tenantID, card.AssignedVehicleID, card.AssignedDriverID, item.TotalAmount, item.VolumeLitres, item.TxnTime, genNotes)
				if genErr == nil && newExpID != "" {
					matchedExpenseID = &newExpID
					status = fuel.ReconStatusSystemGenerated
					result.GeneratedCount++
				}
			}
		}

		notes := item.Notes
		if anomalyNote != nil {
			if notes != nil && *notes != "" {
				combined := *notes + " | " + *anomalyNote
				notes = &combined
			} else {
				notes = anomalyNote
			}
		}

		txn := fuel.FuelCardTransaction{
			TenantID:             tenantID,
			FuelCardID:           card.ID,
			ExternalTxnID:        item.ExternalTxnID,
			TxnTime:              item.TxnTime,
			FuelStationName:      item.FuelStationName,
			FuelStationCity:      item.FuelStationCity,
			FuelType:             item.FuelType,
			VolumeLitres:         item.VolumeLitres,
			RatePerLitre:         item.RatePerLitre,
			TotalAmount:          item.TotalAmount,
			OdometerReported:     item.OdometerReported,
			ReconciliationStatus: status,
			MatchedExpenseID:     matchedExpenseID,
			Notes:                notes,
		}

		// 3. General Ledger Sync (Spec 20 §3.4)
		syncLogID, glErr := uc.repo.PostGeneralLedgerAndSyncLog(ctx, tenantID, txn, string(card.Provider))
		if glErr == nil && syncLogID != "" {
			txn.SyncLogID = &syncLogID
		}

		// 4. Insert Transaction
		inserted, insErr := uc.repo.InsertTransaction(ctx, txn)
		if insErr == nil && inserted != nil {
			result.Transactions = append(result.Transactions, *inserted)
			result.IngestedCount++
		}
	}

	return result, nil
}

// ReconcileTransaction manually links a transaction to an expense claim.
func (uc *FuelCardUseCase) ReconcileTransaction(ctx context.Context, tenantID, txnID string, req fuel.ReconcileTransactionRequest) (*fuel.FuelCardTransaction, error) {
	if req.ExpenseID == "" {
		return nil, errors.New("expense_id is required")
	}
	return uc.repo.ReconcileTransaction(ctx, tenantID, txnID, req.ExpenseID, req.Notes)
}
