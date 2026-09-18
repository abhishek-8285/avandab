package application

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

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

	if err := uc.repo.VerifyAssignmentOwnership(ctx, tenantID, req.AssignedVehicleID, req.AssignedDriverID); err != nil {
		return nil, err
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

// errDuplicateImport marks a replayed external_txn_id: skip silently with no counts.
var errDuplicateImport = errors.New("fuel card transaction already imported")

// SyncTransactions processes statement records through pilferage check, kharcha cross-check, and GL sync.
func (uc *FuelCardUseCase) SyncTransactions(ctx context.Context, tenantID string, req fuel.SyncFuelTransactionsRequest) (*SyncResult, error) {
	result := &SyncResult{}

	for _, item := range req.Transactions {
		if err := uc.syncOne(ctx, tenantID, item, result); err != nil {
			if errors.Is(err, errDuplicateImport) {
				continue
			}
			// Per-item failures stay local: counts only reflect committed
			// work, and the batch keeps its 200 contract.
			continue
		}
	}

	return result, nil
}

// syncOne imports a single statement row atomically: duplicate claim,
// pilferage alert, kharcha match/generate, GL sync log, ledger legs and the
// transaction row commit or roll back together.
func (uc *FuelCardUseCase) syncOne(ctx context.Context, tenantID string, item fuel.IngestTransactionItem, result *SyncResult) error {
	tokenHash := item.CardTokenHash
	if tokenHash == "" && item.CardNumber != "" {
		tokenHash = fuel.HashCardToken(item.CardNumber)
	}
	if tokenHash == "" {
		return errors.New("card token is required")
	}

	card, err := uc.repo.GetCardByTokenHash(ctx, tenantID, tokenHash)
	if err != nil || card == nil {
		// Skip or reject if card not found in this tenant
		return errors.New("fuel card not found in this tenant")
	}

	txnID := uuid.NewString()
	var status fuel.ReconciliationStatus = fuel.ReconStatusUnreconciled
	var matchedExpenseID *string
	var inserted fuel.FuelCardTransaction

	err = uc.repo.WithTransaction(ctx, func(tx fuel.FuelCardRepository) error {
		// Claim the tenant-scoped external key first: replays skip before
		// any pilferage alert, expense, GL or ledger side effect.
		if _, err := tx.GetTransactionByExternalID(ctx, tenantID, item.ExternalTxnID); err == nil {
			return errDuplicateImport
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		anomalyNote, pilferage, err := uc.pilferageCheck(ctx, tx, tenantID, card, item)
		if err != nil {
			return err
		}
		if pilferage {
			status = fuel.ReconStatusFlaggedAnomaly
		}

		// 2. Kharcha Cross-Check (Spec 20 §3.2)
		if status != fuel.ReconStatusFlaggedAnomaly {
			foundExpID, expErr := tx.FindMatchingExpense(ctx, tenantID, card.AssignedVehicleID, card.AssignedDriverID, item.TotalAmount, item.TxnTime)
			switch {
			case expErr == nil && foundExpID != "":
				// Matched existing driver expense claim
				matchedExpenseID = &foundExpID
				status = fuel.ReconStatusMatchedExpense
				if err := tx.MarkExpenseVerified(ctx, tenantID, foundExpID, fmt.Sprintf("Verified via %s fuel card txn %s", card.Provider, item.ExternalTxnID)); err != nil {
					return err
				}
			case expErr != nil && !errors.Is(expErr, sql.ErrNoRows):
				// A real lookup failure is not a miss — fail the item
				// instead of auto-creating a duplicate expense.
				return expErr
			default:
				// Auto-create verified driver expense
				genNotes := fmt.Sprintf("Auto-generated from %s fuel card txn %s at %s", card.Provider, item.ExternalTxnID, item.FuelStationName)
				newExpID, genErr := tx.CreateVerifiedExpense(ctx, tenantID, card.AssignedVehicleID, card.AssignedDriverID, item.TotalAmount, item.VolumeLitres, item.TxnTime, genNotes)
				if genErr != nil {
					return genErr
				}
				if newExpID == "" {
					return errors.New("failed to auto-create verified expense")
				}
				matchedExpenseID = &newExpID
				status = fuel.ReconStatusSystemGenerated
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
			ID:                   txnID,
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

		// 3. General Ledger Sync (Spec 20 §3.4) with the real txn ID so
		// ledger refs and the sync log link to the inserted row.
		syncLogID, err := tx.PostGeneralLedgerAndSyncLog(ctx, tenantID, txn, string(card.Provider))
		if err != nil {
			return err
		}
		txn.SyncLogID = &syncLogID

		// 4. Insert Transaction
		stored, err := tx.InsertTransaction(ctx, txn)
		if err != nil {
			return err
		}
		inserted = *stored
		return nil
	})
	if err != nil {
		return err
	}

	result.Transactions = append(result.Transactions, inserted)
	result.IngestedCount++
	switch status {
	case fuel.ReconStatusMatchedExpense:
		result.MatchedCount++
	case fuel.ReconStatusSystemGenerated:
		result.GeneratedCount++
	case fuel.ReconStatusFlaggedAnomaly:
		result.AnomalyCount++
	}
	return nil
}

// pilferageCheck applies the tank-capacity guard, recording the alert inside
// the caller's transaction. It returns the anomaly note and whether the item
// is flagged.
func (uc *FuelCardUseCase) pilferageCheck(ctx context.Context, tx fuel.FuelCardRepository, tenantID string, card *fuel.FuelCard, item fuel.IngestTransactionItem) (*string, bool, error) {
	// 1. Pilferage Guard: Check vehicle tank capacity
	if card.AssignedVehicleID != nil && *card.AssignedVehicleID != "" {
		tankCap, err := tx.GetVehicleTankCapacity(ctx, tenantID, *card.AssignedVehicleID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, false, nil
			}
			return nil, false, err
		}
		if tankCap > 0 && item.VolumeLitres > (tankCap*1.05) {
			msg := fmt.Sprintf("Fuel volume (%.1fL) exceeds vehicle tank capacity (%.1fL + 5%% tolerance)", item.VolumeLitres, tankCap)
			if err := tx.RecordPilferageAlert(ctx, tenantID, *card.AssignedVehicleID, "Fuel Volume Pilferage Risk", msg); err != nil {
				return nil, false, err
			}
			return &msg, true, nil
		}
	}
	return nil, false, nil
}

// ReconcileTransaction manually links a transaction to an expense claim.
func (uc *FuelCardUseCase) ReconcileTransaction(ctx context.Context, tenantID, txnID string, req fuel.ReconcileTransactionRequest) (*fuel.FuelCardTransaction, error) {
	if req.ExpenseID == "" {
		return nil, errors.New("expense_id is required")
	}
	return uc.repo.ReconcileTransaction(ctx, tenantID, txnID, req.ExpenseID, req.Notes)
}
