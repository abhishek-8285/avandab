package application

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"transport-app/internal/settlement/domain"
)

type SettlementAppService struct {
	repo           domain.SettlementRepository
	webhookSecret  string
	minPayoutLimit float64
}

func NewSettlementAppService(repo domain.SettlementRepository, webhookSecret string, minPayoutLimit float64) *SettlementAppService {
	if minPayoutLimit <= 0 {
		minPayoutLimit = 100.0 // Min ₹100 payout threshold
	}
	return &SettlementAppService{
		repo:           repo,
		webhookSecret:  webhookSecret,
		minPayoutLimit: minPayoutLimit,
	}
}

type CalculateSettlementRequest struct {
	TripID            string  `json:"trip_id"`
	DriverID          string  `json:"driver_id"`
	GrossFare         float64 `json:"gross_fare"`
	TollAdjustment    float64 `json:"toll_adjustment"`
	AdvanceDeductions float64 `json:"advance_deductions"`
	CommissionRate    float64 `json:"commission_rate,omitempty"`
	TDSRate           float64 `json:"tds_rate,omitempty"`
}

func (s *SettlementAppService) CalculateAndCreateSettlement(ctx context.Context, tenantID string, req CalculateSettlementRequest) (*domain.Settlement, error) {
	if tenantID == "" || req.TripID == "" || req.DriverID == "" {
		return nil, errors.New("tenant_id, trip_id, and driver_id are required")
	}

	// 1. Idempotency / Concurrency Guard: Check if settlement already exists
	existing, err := s.repo.GetSettlementByTripID(ctx, tenantID, req.TripID)
	if err == nil && existing != nil {
		return existing, nil
	}

	settlementID := "set_" + uuid.NewString()
	settlement := domain.CalculateSettlement(
		settlementID, tenantID, req.TripID, req.DriverID,
		req.GrossFare, req.TollAdjustment, req.AdvanceDeductions,
		req.CommissionRate, req.TDSRate,
	)

	// Persist settlement record
	if err := s.repo.CreateSettlement(ctx, tenantID, settlement); err != nil {
		existing, getErr := s.repo.GetSettlementByTripID(ctx, tenantID, req.TripID)
		if getErr == nil && existing != nil {
			return existing, nil
		}
		return nil, fmt.Errorf("failed creating settlement: %w", err)
	}

	// Append immutable ledger entries for driver earnings & deductions
	tripRef := req.TripID

	// Credit: Gross Trip Earning
	_ = s.repo.AppendLedgerEntry(ctx, tenantID, &domain.LedgerEntry{
		ID:            "led_" + uuid.NewString(),
		TenantID:      tenantID,
		DriverID:      req.DriverID,
		TripID:        &tripRef,
		EntryType:     domain.EntryTripEarning,
		Amount:        settlement.GrossFare,
		Currency:      "INR",
		ReferenceType: "settlement",
		ReferenceID:   settlement.ID,
		Description:   fmt.Sprintf("Trip %s gross fare", req.TripID),
	})

	// Debit: Platform Commission
	if settlement.CommissionAmount > 0 {
		_ = s.repo.AppendLedgerEntry(ctx, tenantID, &domain.LedgerEntry{
			ID:            "led_" + uuid.NewString(),
			TenantID:      tenantID,
			DriverID:      req.DriverID,
			TripID:        &tripRef,
			EntryType:     domain.EntryCommission,
			Amount:        -settlement.CommissionAmount,
			Currency:      "INR",
			ReferenceType: "settlement",
			ReferenceID:   settlement.ID,
			Description:   fmt.Sprintf("Platform commission (%.1f%%)", settlement.CommissionRate*100),
		})
	}

	// Credit: Toll Adjustment
	if settlement.TollAdjustment > 0 {
		_ = s.repo.AppendLedgerEntry(ctx, tenantID, &domain.LedgerEntry{
			ID:            "led_" + uuid.NewString(),
			TenantID:      tenantID,
			DriverID:      req.DriverID,
			TripID:        &tripRef,
			EntryType:     domain.EntryTollAdjustment,
			Amount:        settlement.TollAdjustment,
			Currency:      "INR",
			ReferenceType: "settlement",
			ReferenceID:   settlement.ID,
			Description:   "FASTag / Toll reimbursement",
		})
	}

	// Debit: Advance Deduction
	if settlement.AdvanceDeductions > 0 {
		_ = s.repo.AppendLedgerEntry(ctx, tenantID, &domain.LedgerEntry{
			ID:            "led_" + uuid.NewString(),
			TenantID:      tenantID,
			DriverID:      req.DriverID,
			TripID:        &tripRef,
			EntryType:     domain.EntryAdvanceDeduction,
			Amount:        -settlement.AdvanceDeductions,
			Currency:      "INR",
			ReferenceType: "settlement",
			ReferenceID:   settlement.ID,
			Description:   "Fuel / Cash advance deduction",
		})
	}

	// Debit: TDS Deduction
	if settlement.TDSAmount > 0 {
		_ = s.repo.AppendLedgerEntry(ctx, tenantID, &domain.LedgerEntry{
			ID:            "led_" + uuid.NewString(),
			TenantID:      tenantID,
			DriverID:      req.DriverID,
			TripID:        &tripRef,
			EntryType:     domain.EntryPenalty, // tax withhold
			Amount:        -settlement.TDSAmount,
			Currency:      "INR",
			ReferenceType: "settlement",
			ReferenceID:   settlement.ID,
			Description:   fmt.Sprintf("TDS deduction (Sec 194C %.1f%%)", settlement.TDSRate*100),
		})
	}

	return settlement, nil
}

func (s *SettlementAppService) GetDriverWallet(ctx context.Context, tenantID, driverID string) (*domain.DriverWallet, error) {
	if tenantID == "" || driverID == "" {
		return nil, errors.New("tenant_id and driver_id are required")
	}
	return s.repo.GetDriverWallet(ctx, tenantID, driverID)
}

type InitiatePayoutRequest struct {
	IdempotencyKey string  `json:"idempotency_key"`
	Amount         float64 `json:"amount"`
}

type PayoutResponse struct {
	PayoutID       string              `json:"payout_id"`
	DriverID       string              `json:"driver_id"`
	Amount         float64             `json:"amount"`
	Status         domain.PayoutStatus `json:"status"`
	IdempotencyKey string              `json:"idempotency_key"`
	IsDuplicate    bool                `json:"is_duplicate,omitempty"`
}

func (s *SettlementAppService) InitiatePayout(ctx context.Context, tenantID, driverID string, req InitiatePayoutRequest) (*PayoutResponse, error) {
	if tenantID == "" || driverID == "" {
		return nil, errors.New("tenant_id and driver_id are required")
	}
	if req.IdempotencyKey == "" {
		return nil, errors.New("idempotency_key is required")
	}

	// 1. Idempotency Check: Return existing payout if duplicate key
	existing, err := s.repo.GetPayoutByIdempotencyKey(ctx, tenantID, req.IdempotencyKey)
	if err == nil && existing != nil {
		return &PayoutResponse{
			PayoutID:       existing.ID,
			DriverID:       existing.DriverID,
			Amount:         existing.Amount,
			Status:         existing.Status,
			IdempotencyKey: existing.IdempotencyKey,
			IsDuplicate:    true,
		}, nil
	}

	// 2. Bank Account Verification Check
	verified, accountID, err := s.repo.IsDriverPayoutAccountVerified(ctx, tenantID, driverID)
	if err != nil || !verified {
		return nil, errors.New("unverified bank account: driver must have an active verified payout account")
	}

	// 3. Balance Check
	wallet, err := s.repo.GetDriverWallet(ctx, tenantID, driverID)
	if err != nil {
		return nil, err
	}

	if err := domain.ValidatePayoutEligibility(wallet.AvailableBalance, req.Amount, verified, s.minPayoutLimit); err != nil {
		return nil, err
	}

	payoutID := "pout_" + uuid.NewString()
	payout := &domain.PayoutInstruction{
		ID:              payoutID,
		TenantID:        tenantID,
		DriverID:        driverID,
		PayoutAccountID: accountID,
		Amount:          req.Amount,
		Currency:        "INR",
		IdempotencyKey:  req.IdempotencyKey,
		Status:          domain.PayoutInitiated,
		InitiatedAt:     time.Now(),
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	if err := s.repo.CreatePayoutInstruction(ctx, tenantID, payout); err != nil {
		return nil, fmt.Errorf("failed creating payout instruction: %w", err)
	}

	// 4. Debit driver ledger for held payout amount
	_ = s.repo.AppendLedgerEntry(ctx, tenantID, &domain.LedgerEntry{
		ID:            "led_" + uuid.NewString(),
		TenantID:      tenantID,
		DriverID:      driverID,
		EntryType:     domain.EntryPayout,
		Amount:        -req.Amount,
		Currency:      "INR",
		ReferenceType: "payout",
		ReferenceID:   payoutID,
		Description:   fmt.Sprintf("Disbursement payout %s initiated", payoutID),
	})

	return &PayoutResponse{
		PayoutID:       payout.ID,
		DriverID:       driverID,
		Amount:         payout.Amount,
		Status:         payout.Status,
		IdempotencyKey: payout.IdempotencyKey,
		IsDuplicate:    false,
	}, nil
}

type RazorpayWebhookPayload struct {
	Event   string `json:"event"`
	Payload struct {
		Payout struct {
			Entity struct {
				ID        string  `json:"id"`
				Amount    float64 `json:"amount"`
				Currency  string  `json:"currency"`
				Status    string  `json:"status"` // processed, reversed, failed
				UTR       string  `json:"utr"`
				Reference string  `json:"reference_id"` // our payoutID
				ErrorDesc string  `json:"error_description"`
			} `json:"entity"`
		} `json:"payout"`
	} `json:"payload"`
}

func (s *SettlementAppService) ProcessProviderWebhook(ctx context.Context, tenantID, providerEventID, signature string, body []byte) error {
	if providerEventID == "" {
		return errors.New("provider_event_id is required")
	}

	// 1. Signature Verification: Mandatory if webhook secret configured
	if s.webhookSecret != "" {
		if signature == "" {
			return errors.New("missing webhook signature: signature is mandatory")
		}
		mac := hmac.New(sha256.New, []byte(s.webhookSecret))
		mac.Write(body)
		expectedSignature := hex.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(signature), []byte(expectedSignature)) {
			return errors.New("invalid webhook signature")
		}
	}

	// 2. Webhook Idempotency Check (globally unique by provider + provider_event_id)
	processed, err := s.repo.IsProviderEventProcessed(ctx, "razorpay", providerEventID)
	if err == nil && processed {
		return nil // idempotent 200 OK
	}

	var data RazorpayWebhookPayload
	if err := json.Unmarshal(body, &data); err != nil {
		return fmt.Errorf("invalid webhook JSON: %w", err)
	}

	entity := data.Payload.Payout.Entity
	payoutID := entity.Reference
	if payoutID == "" {
		return errors.New("missing payout reference_id in webhook payload")
	}

	// 3. Authoritative Tenant & Payout Lookup: Never trust caller/webhook tenant blindly
	payout, err := s.repo.GetPayoutByIDGlobal(ctx, payoutID)
	if err != nil || payout == nil {
		return fmt.Errorf("payout lookup error: %w", err)
	}
	tenantID = payout.TenantID

	// 4. Financial Integrity: Validate amount and currency against local instruction
	if entity.Amount > 0 {
		amtDiffDirect := entity.Amount - payout.Amount
		amtDiffPaise := (entity.Amount / 100.0) - payout.Amount
		if amtDiffDirect < 0 {
			amtDiffDirect = -amtDiffDirect
		}
		if amtDiffPaise < 0 {
			amtDiffPaise = -amtDiffPaise
		}
		if amtDiffDirect > 0.01 && amtDiffPaise > 0.01 {
			return fmt.Errorf("financial mismatch: webhook amount %.2f does not match expected payout amount %.2f", entity.Amount, payout.Amount)
		}
	}
	if entity.Currency != "" && payout.Currency != "" && !strings.EqualFold(entity.Currency, payout.Currency) {
		return fmt.Errorf("currency mismatch: webhook currency %s does not match expected %s", entity.Currency, payout.Currency)
	}

	// 5. Provider Payout ID Integrity check
	if payout.ProviderPayoutID != nil && *payout.ProviderPayoutID != "" && entity.ID != "" && *payout.ProviderPayoutID != entity.ID {
		return fmt.Errorf("provider payout ID mismatch: expected %s, got %s", *payout.ProviderPayoutID, entity.ID)
	}

	// 6. Reconcile based on external rail outcome
	var newStatus domain.PayoutStatus
	var utr *string
	var failReason *string

	if entity.UTR != "" {
		utr = &entity.UTR
	}
	if entity.ErrorDesc != "" {
		failReason = &entity.ErrorDesc
	}

	switch entity.Status {
	case "processed":
		newStatus = domain.PayoutPaid
	case "failed":
		newStatus = domain.PayoutFailed
	case "reversed":
		newStatus = domain.PayoutReversed
	default:
		newStatus = domain.PayoutProcessing
	}

	// Validate status transition state machine
	if err := payout.CanTransitionTo(newStatus); err != nil {
		return err
	}

	// Update payout status
	provPayoutID := entity.ID
	if err := s.repo.UpdatePayoutStatus(ctx, tenantID, payout.ID, newStatus, &provPayoutID, utr, failReason); err != nil {
		return fmt.Errorf("failed updating payout status: %w", err)
	}

	// 7. Compensating Ledger Entry on Failure or Reversal (strictly exactly ONE compensating credit)
	if newStatus == domain.PayoutFailed || newStatus == domain.PayoutReversed {
		hasComp, err := s.repo.HasCompensatingLedgerEntry(ctx, tenantID, "payout_reversal", payout.ID)
		if err == nil && !hasComp {
			_ = s.repo.AppendLedgerEntry(ctx, tenantID, &domain.LedgerEntry{
				ID:            "led_" + uuid.NewString(),
				TenantID:      tenantID,
				DriverID:      payout.DriverID,
				EntryType:     domain.EntryPayoutReversal,
				Amount:        payout.Amount, // Credit back original amount
				Currency:      payout.Currency,
				ReferenceType: "payout_reversal",
				ReferenceID:   payout.ID,
				Description:   fmt.Sprintf("Compensating credit for %s payout %s", newStatus, payout.ID),
			})
		}
	}

	// 8. Record event in idempotency log
	_ = s.repo.RecordProviderEvent(ctx, tenantID, "razorpay", providerEventID, data.Event, string(body))

	return nil
}
