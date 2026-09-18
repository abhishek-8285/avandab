package application

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
	// Ownership: wallet/payout rail. This flow owns driver wallet balances and
	// payout_instructions (statuses pending/calculated/approved/payable/paid).
	// The legacy trip-close accounting UI flow (internal/service
	// DriverSettlementService.GenerateSettlement, statuses
	// pending/processing/paid/disputed + settlement_lines breakdown) writes the
	// SAME driver_settlements/driver_ledger_entries tables — tables are NOT
	// merged (migration edits forbidden). See warnIfLegacySettlement guard below.
	if tenantID == "" || req.TripID == "" || req.DriverID == "" {
		return nil, errors.New("tenant_id, trip_id, and driver_id are required")
	}

	// Gap-2 dual-write guard (one-directional, soft): cross-check the
	// legacy-only settlement_lines table. Warn-only, never fail hard — a missing
	// table or query error must not break prod settlement creation.
	s.warnIfLegacySettlement(ctx, req.TripID)

	// 1. Idempotency / Concurrency Guard: Check if settlement already exists
	existing, err := s.repo.GetSettlementByTripID(ctx, tenantID, req.TripID)
	if err == nil && existing != nil {
		// Crash-partial recovery: a header committed but ledger appends
		// never landed (crash between the two) leaves the wallet
		// understated, and this early return would keep it that way.
		// Heal only our own headers (set_ prefix) with no legacy
		// breakdown - legacy-owned rows must never gain wallet entries.
		s.backfillOwnLedger(ctx, tenantID, existing)
		return existing, nil
	}

	settlementID := "set_" + uuid.NewString()
	settlement := domain.CalculateSettlement(
		settlementID, tenantID, req.TripID, req.DriverID,
		req.GrossFare, req.TollAdjustment, req.AdvanceDeductions,
		req.CommissionRate, req.TDSRate,
	)

	// Persist settlement record and ledger entries atomically: a crash
	// between header and lines must not leave an orphan header behind.
	var out *domain.Settlement
	err = s.repo.WithTransaction(ctx, func(ctx context.Context) error {
		if err := s.repo.CreateSettlement(ctx, tenantID, settlement); err != nil {
			existing, getErr := s.repo.GetSettlementByTripID(ctx, tenantID, req.TripID)
			if getErr == nil && existing != nil {
				out = existing
				return nil
			}
			return fmt.Errorf("failed creating settlement: %w", err)
		}

		// Append immutable ledger entries for driver earnings & deductions.
		// Single builder for create + backfill paths so both write identical sets.
		if err := s.appendSettlementLedger(ctx, tenantID, ledgerAmounts{
			driverID: req.DriverID, tripID: req.TripID, settlementID: settlement.ID,
			gross: settlement.GrossFare, commission: settlement.CommissionAmount,
			commissionRate: settlement.CommissionRate, toll: settlement.TollAdjustment,
			advance: settlement.AdvanceDeductions, tds: settlement.TDSAmount,
			tdsRate: settlement.TDSRate,
		}, nil); err != nil {
			return err
		}
		out = settlement
		return nil
	})
	if err != nil {
		return nil, err
	}

	return out, nil
}

// ledgerAmounts carries one settlement's money movement for ledger writes.
type ledgerAmounts struct {
	driverID, tripID, settlementID                   string
	gross, commission, commissionRate, toll, advance float64
	tds, tdsRate                                     float64
}

// expectedEntryTypes mirrors the append predicates below so backfill and
// create paths can never drift apart.
func (a ledgerAmounts) expectedEntryTypes() []domain.EntryType {
	var out []domain.EntryType
	if a.gross > 0 {
		out = append(out, domain.EntryTripEarning)
	}
	if a.commission > 0 {
		out = append(out, domain.EntryCommission)
	}
	if a.toll > 0 {
		out = append(out, domain.EntryTollAdjustment)
	}
	if a.advance > 0 {
		out = append(out, domain.EntryAdvanceDeduction)
	}
	if a.tds > 0 {
		out = append(out, domain.EntryPenalty) // tax withhold
	}
	return out
}

// appendSettlementLedger writes every expected entry except those in have
// (empty on create; stored types on backfill). Predicates live in exactly
// one place — expectedEntryTypes above.
func (s *SettlementAppService) appendSettlementLedger(ctx context.Context, tenantID string, a ledgerAmounts, have map[domain.EntryType]bool) error {
	appendOne := func(typ domain.EntryType, amount float64, desc string) error {
		if have[typ] {
			return nil
		}
		if err := s.repo.AppendLedgerEntry(ctx, tenantID, &domain.LedgerEntry{
			ID:            "led_" + uuid.NewString(),
			TenantID:      tenantID,
			DriverID:      a.driverID,
			TripID:        &a.tripID,
			EntryType:     typ,
			Amount:        amount,
			Currency:      "INR",
			ReferenceType: "settlement",
			ReferenceID:   a.settlementID,
			Description:   desc,
		}); err != nil {
			return fmt.Errorf("append settlement ledger entry: %w", err)
		}
		return nil
	}

	// Credit: Gross Trip Earning
	if a.gross > 0 {
		if err := appendOne(domain.EntryTripEarning, a.gross,
			fmt.Sprintf("Trip %s gross fare", a.tripID)); err != nil {
			return err
		}
	}

	// Debit: Platform Commission
	if a.commission > 0 {
		if err := appendOne(domain.EntryCommission, -a.commission,
			fmt.Sprintf("Platform commission (%.1f%%)", a.commissionRate*100)); err != nil {
			return err
		}
	}

	// Credit: Toll Adjustment
	if a.toll > 0 {
		if err := appendOne(domain.EntryTollAdjustment, a.toll,
			"FASTag / Toll reimbursement"); err != nil {
			return err
		}
	}

	// Debit: Advance Deduction
	if a.advance > 0 {
		if err := appendOne(domain.EntryAdvanceDeduction, -a.advance,
			"Fuel / Cash advance deduction"); err != nil {
			return err
		}
	}

	// Debit: TDS Deduction
	if a.tds > 0 {
		if err := appendOne(domain.EntryPenalty, -a.tds,
			fmt.Sprintf("TDS deduction (Sec 194C %.1f%%)", a.tdsRate*100)); err != nil {
			return err
		}
	}

	return nil
}

// warnIfLegacySettlement performs the gap-2 dual-write cross-check: a single
// SELECT against settlement_lines, which only the legacy trip-close flow
// writes. On a hit it logs a warning (legacy owns this trip — the existing
// idempotency guard above reuses the single driver_settlements row instead of
// appending duplicate ledger entries). Fail-open by design: any query error
// (e.g. table absent) is logged and settlement creation proceeds, so this guard
// can never break prod. Tenant is intentionally not consulted here:
// settlement_lines carries no tenant column and trip IDs are trip-scoped.
func (s *SettlementAppService) warnIfLegacySettlement(ctx context.Context, tripID string) {
	legacy, err := s.repo.HasLegacySettlementLines(ctx, tripID)
	if err != nil {
		slog.Default().Warn("settlement dual-write cross-check unavailable, proceeding",
			"trip_id", tripID, "error", err)
		return
	}
	if legacy {
		slog.Default().Warn("settlement dual-write: legacy trip-close breakdown exists, reusing single settlement row",
			"trip_id", tripID)
	}
}

// backfillOwnLedger heals crash-partials: a wallet-rail header (set_ prefix)
// whose ledger appends never landed. Fail-open throughout — any uncertainty
// returns without writing, so this can never duplicate money, only skip:
//   - non-set_ headers: owned by legacy/bridge flows, never ours.
//   - legacy breakdown present: legacy owns the trip, reuse as-is.
//   - list error: unknown state, skip (logged).
//
// Concurrent backfills share a tiny duplicate window (no unique constraint
// on entry types — legacy writes two PENALTY rows, so none can exist);
// entries stay individually visible in the ledger, never silent.
func (s *SettlementAppService) backfillOwnLedger(ctx context.Context, tenantID string, existing *domain.Settlement) {
	if existing == nil || !strings.HasPrefix(existing.ID, "set_") {
		return
	}
	legacy, err := s.repo.HasLegacySettlementLines(ctx, existing.TripID)
	if err == nil && legacy {
		return
	}
	stored, err := s.repo.ListLedgerEntryTypes(ctx, tenantID, "settlement", existing.ID)
	if err != nil {
		slog.Default().Warn("settlement backfill cross-check unavailable, skipping",
			"trip_id", existing.TripID, "error", err)
		return
	}
	have := make(map[domain.EntryType]bool, len(stored))
	for _, t := range stored {
		have[t] = true
	}
	complete := true
	want := ledgerAmounts{
		driverID: existing.DriverID, tripID: existing.TripID, settlementID: existing.ID,
		gross: existing.GrossFare, commission: existing.CommissionAmount,
		commissionRate: existing.CommissionRate, toll: existing.TollAdjustment,
		advance: existing.AdvanceDeductions, tds: existing.TDSAmount,
		tdsRate: existing.TDSRate,
	}.expectedEntryTypes()
	for _, t := range want {
		if !have[t] {
			complete = false
			break
		}
	}
	if complete {
		return
	}
	if err := s.appendSettlementLedger(ctx, tenantID, ledgerAmounts{
		driverID: existing.DriverID, tripID: existing.TripID, settlementID: existing.ID,
		gross: existing.GrossFare, commission: existing.CommissionAmount,
		commissionRate: existing.CommissionRate, toll: existing.TollAdjustment,
		advance: existing.AdvanceDeductions, tds: existing.TDSAmount,
		tdsRate: existing.TDSRate,
	}, have); err != nil {
		slog.Default().Error("settlement ledger backfill failed",
			"trip_id", existing.TripID, "settlement_id", existing.ID, "error", err)
		return
	}
	slog.Default().Info("settlement ledger backfilled after partial write",
		"trip_id", existing.TripID, "settlement_id", existing.ID)
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

	duplicateOf := func(existing *domain.PayoutInstruction) *PayoutResponse {
		return &PayoutResponse{
			PayoutID:       existing.ID,
			DriverID:       existing.DriverID,
			Amount:         existing.Amount,
			Status:         existing.Status,
			IdempotencyKey: existing.IdempotencyKey,
			IsDuplicate:    true,
		}
	}

	// 1. Idempotency Check: Return existing payout if duplicate key
	existing, err := s.repo.GetPayoutByIdempotencyKey(ctx, tenantID, req.IdempotencyKey)
	if err == nil && existing != nil {
		return duplicateOf(existing), nil
	}

	// Instruction insert and wallet debit commit as one unit: a debit
	// failure must not leave a payable instruction behind, and a retry
	// must converge to exactly one debit.
	var resp *PayoutResponse
	err = s.repo.WithTransaction(ctx, func(ctx context.Context) error {
		// Serialize concurrent same-driver payouts before any read: without
		// the driver lock both readers see the full balance and both debits
		// commit (lost update). The peer waits here until this transaction
		// commits, then reads the winner's debit.
		if err := s.repo.LockDriverPayout(ctx, tenantID, driverID); err != nil {
			return err
		}

		// Re-check inside the transaction: a concurrent same-key request
		// may have committed between the fast-path check and our BEGIN.
		if existing, err := s.repo.GetPayoutByIdempotencyKey(ctx, tenantID, req.IdempotencyKey); err == nil && existing != nil {
			resp = duplicateOf(existing)
			return nil
		}

		// 2. Bank Account Verification Check
		verified, accountID, err := s.repo.IsDriverPayoutAccountVerified(ctx, tenantID, driverID)
		if err != nil || !verified {
			return errors.New("unverified bank account: driver must have an active verified payout account")
		}

		// 3. Balance Check
		wallet, err := s.repo.GetDriverWallet(ctx, tenantID, driverID)
		if err != nil {
			return err
		}

		if err := domain.ValidatePayoutEligibility(wallet.AvailableBalance, req.Amount, verified, s.minPayoutLimit); err != nil {
			return err
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
			// Lost the insert race: another request committed this key.
			// Roll back and report the winner as a duplicate outside.
			if isUniqueConflict(err) {
				return errPayoutKeyConflict
			}
			return fmt.Errorf("failed creating payout instruction: %w", err)
		}

		// 4. Debit driver ledger for held payout amount
		if err := s.repo.AppendLedgerEntry(ctx, tenantID, &domain.LedgerEntry{
			ID:            "led_" + uuid.NewString(),
			TenantID:      tenantID,
			DriverID:      driverID,
			EntryType:     domain.EntryPayout,
			Amount:        -req.Amount,
			Currency:      "INR",
			ReferenceType: "payout",
			ReferenceID:   payoutID,
			Description:   fmt.Sprintf("Disbursement payout %s initiated", payoutID),
		}); err != nil {
			return fmt.Errorf("append payout ledger entry: %w", err)
		}

		resp = &PayoutResponse{
			PayoutID:       payout.ID,
			DriverID:       driverID,
			Amount:         payout.Amount,
			Status:         payout.Status,
			IdempotencyKey: payout.IdempotencyKey,
			IsDuplicate:    false,
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, errPayoutKeyConflict) {
			if existing, gerr := s.repo.GetPayoutByIdempotencyKey(ctx, tenantID, req.IdempotencyKey); gerr == nil && existing != nil {
				return duplicateOf(existing), nil
			}
		}
		return nil, err
	}

	return resp, nil
}

// errPayoutKeyConflict signals a lost idempotency-key insert race. The
// enclosing transaction rolls back; the caller re-reads the winner outside
// (a constraint violation may have aborted the tx on Postgres).
var errPayoutKeyConflict = errors.New("payout idempotency key conflict")

// isUniqueConflict reports unique-constraint violations across engines.
func isUniqueConflict(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "duplicate key value violates unique constraint")
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

// ProcessProviderWebhook reconciles a signed Razorpay payout callback.
// The caller-supplied tenant is intentionally ignored (kept in the signature as
// `_` for API stability): provider callbacks arrive on an unauthenticated route
// (POST /api/v1/webhooks/payouts/razorpay) so there is no trustworthy request
// tenant. Tenancy is derived authoritatively from the payout row below, and
// every downstream write is scoped to that value.
func (s *SettlementAppService) ProcessProviderWebhook(ctx context.Context, _, providerEventID, signature string, body []byte) error {
	if providerEventID == "" {
		return errors.New("provider_event_id is required")
	}

	// 1. Signature Verification: fail closed without a secret (C3) — an
	// unverified payout webhook can forge PAID outcomes. Mirrors the payment
	// webhook's ErrWebhookNotConfigured.
	if s.webhookSecret == "" {
		return errors.New("webhook secret not configured")
	}
	if signature == "" {
		return errors.New("missing webhook signature: signature is mandatory")
	}
	mac := hmac.New(sha256.New, []byte(s.webhookSecret))
	mac.Write(body)
	expectedSignature := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(signature), []byte(expectedSignature)) {
		return errors.New("invalid webhook signature")
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
	// Authoritative tenant: comes from the stored payout, never from the caller.
	tenantID := payout.TenantID

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

	// Update payout status, compensating credit and processed marker as one
	// unit: a failed credit must not leave the payout marked reversed.
	provPayoutID := entity.ID
	if err := s.repo.WithTransaction(ctx, func(ctx context.Context) error {
		if err := s.repo.UpdatePayoutStatus(ctx, tenantID, payout.ID, newStatus, &provPayoutID, utr, failReason); err != nil {
			return fmt.Errorf("failed updating payout status: %w", err)
		}

		// 7. Compensating Ledger Entry on Failure or Reversal (strictly exactly ONE compensating credit)
		if newStatus == domain.PayoutFailed || newStatus == domain.PayoutReversed {
			hasComp, err := s.repo.HasCompensatingLedgerEntry(ctx, tenantID, "payout_reversal", payout.ID)
			if err != nil {
				return fmt.Errorf("check compensating ledger entry: %w", err)
			}
			if !hasComp {
				if err := s.repo.AppendLedgerEntry(ctx, tenantID, &domain.LedgerEntry{
					ID:            "led_" + uuid.NewString(),
					TenantID:      tenantID,
					DriverID:      payout.DriverID,
					EntryType:     domain.EntryPayoutReversal,
					Amount:        payout.Amount, // Credit back original amount
					Currency:      payout.Currency,
					ReferenceType: "payout_reversal",
					ReferenceID:   payout.ID,
					Description:   fmt.Sprintf("Compensating credit for %s payout %s", newStatus, payout.ID),
				}); err != nil {
					return fmt.Errorf("append compensating ledger entry: %w", err)
				}
			}
		}

		// 8. Record event in idempotency log
		if err := s.repo.RecordProviderEvent(ctx, tenantID, "razorpay", providerEventID, data.Event, string(body)); err != nil {
			// A concurrent delivery already recorded this event; its own
			// transaction applied the same status and credit.
			if isUniqueConflict(err) {
				return nil
			}
			return fmt.Errorf("record provider event: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}

	return nil
}
