package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"transport-app/internal/secure"
	"transport-app/internal/settlement/domain"
	"transport-app/internal/settlement/payout"
)

// SetPayoutProvider wires the real payout rail. InitiatePayout stays disabled
// unless the rail is also explicitly enabled.
func (s *SettlementAppService) SetPayoutProvider(p payout.Provider) { s.payoutProvider = p }

// SetPayoutWebhookSecret overrides the webhook signing secret for payout
// callbacks (falls back to the shared Razorpay webhook secret).
func (s *SettlementAppService) SetPayoutWebhookSecret(secret string) { s.payoutWebhookSecret = secret }

func (s *SettlementAppService) payoutSecret() string {
	if s.payoutWebhookSecret != "" {
		return s.payoutWebhookSecret
	}
	return s.webhookSecret
}

// PayoutAccountVerification is returned by VerifyPayoutAccount.
type PayoutAccountVerification struct {
	AccountID string `json:"account_id"`
	Status    string `json:"status"`
	Reference string `json:"reference"`
}

// VerifyPayoutAccount starts penny-drop validation for the driver's primary
// account. The final valid/invalid arrives via the fund_account.validation
// webhook, which flips verification_status.
func (s *SettlementAppService) VerifyPayoutAccount(ctx context.Context, tenantID, driverID string) (*PayoutAccountVerification, error) {
	if s.payoutProvider == nil {
		return nil, ErrPayoutRailDisabled
	}
	_, accountID, err := s.repo.IsDriverPayoutAccountVerified(ctx, tenantID, driverID)
	if err != nil {
		return nil, err
	}
	if accountID == "" {
		return nil, errors.New("no payout account on file for driver")
	}
	acct, err := s.repo.GetPayoutAccount(ctx, tenantID, accountID)
	if err != nil {
		return nil, err
	}
	plain, err := secure.DecryptAccountNumber(acct.AccountNumberEncrypted)
	if err != nil {
		return nil, fmt.Errorf("decrypt payout account: %w", err)
	}
	res, err := s.payoutProvider.VerifyBankAccount(ctx, payout.VerifyRequest{
		TenantID: tenantID, HolderName: acct.HolderName, AccountNumber: plain, IFSC: acct.IFSC,
	})
	if err != nil {
		return nil, fmt.Errorf("penny-drop validation: %w", err)
	}
	if err := s.repo.UpdatePayoutAccountVerification(ctx, tenantID, acct.ID, "penny_drop_pending", res.FundAccountID, ""); err != nil {
		return nil, err
	}
	return &PayoutAccountVerification{AccountID: acct.ID, Status: "penny_drop_pending", Reference: res.FundAccountID}, nil
}

// dispatchPayout sends a committed payout instruction to the provider and
// records the outcome. On failure the instruction is marked failed and the
// wallet debit is compensated exactly once.
func (s *SettlementAppService) dispatchPayout(ctx context.Context, tenantID, payoutID, driverID, accountID string, amount float64) error {
	acct, err := s.repo.GetPayoutAccount(ctx, tenantID, accountID)
	if err != nil {
		reason := fmt.Sprintf("load payout account: %v", err)
		_ = s.failPayout(ctx, tenantID, payoutID, driverID, amount, "INR", nil, reason)
		return errors.New(reason)
	}
	plain, err := secure.DecryptAccountNumber(acct.AccountNumberEncrypted)
	if err != nil {
		reason := fmt.Sprintf("decrypt payout account: %v", err)
		_ = s.failPayout(ctx, tenantID, payoutID, driverID, amount, "INR", nil, reason)
		return errors.New(reason)
	}
	res, err := s.payoutProvider.CreatePayout(ctx, payout.CreateRequest{
		TenantID: tenantID, PayoutID: payoutID, DriverID: driverID,
		HolderName: acct.HolderName, AccountNumber: plain, IFSC: acct.IFSC, AmountINR: amount,
	})
	if err != nil {
		if ferr := s.failPayout(ctx, tenantID, payoutID, driverID, amount, "INR", nil, err.Error()); ferr != nil {
			return fmt.Errorf("payout provider: %w; compensation failed: %v", err, ferr)
		}
		return fmt.Errorf("payout provider: %w", err)
	}
	status := mapProviderStatus(res.Status)
	providerID := res.ProviderPayoutID
	var utr, failReason *string
	if res.UTR != "" {
		utr = &res.UTR
	}
	if res.FailureReason != "" {
		failReason = &res.FailureReason
	}
	if err := s.repo.UpdatePayoutStatus(ctx, tenantID, payoutID, status, &providerID, utr, failReason); err != nil {
		return fmt.Errorf("record provider payout status: %w", err)
	}
	if status == domain.PayoutFailed || status == domain.PayoutReversed {
		if err := s.compensatePayout(ctx, tenantID, payoutID, driverID, amount, "INR", status); err != nil {
			return err
		}
	}
	slog.Default().Info("payout rail dispatched",
		"payout_id", payoutID, "provider_payout_id", providerID, "status", string(status))
	return nil
}

// failPayout marks a payout failed and credits the wallet back exactly once.
func (s *SettlementAppService) failPayout(ctx context.Context, tenantID, payoutID, driverID string, amount float64, currency string, providerID *string, reason string) error {
	var failReason *string
	if reason != "" {
		failReason = &reason
	}
	if err := s.repo.UpdatePayoutStatus(ctx, tenantID, payoutID, domain.PayoutFailed, providerID, nil, failReason); err != nil {
		return err
	}
	return s.compensatePayout(ctx, tenantID, payoutID, driverID, amount, currency, domain.PayoutFailed)
}

// compensatePayout credits the wallet back for a payout that did not reach the
// driver. Idempotent: exactly one reversal entry per payout.
func (s *SettlementAppService) compensatePayout(ctx context.Context, tenantID, payoutID, driverID string, amount float64, currency string, status domain.PayoutStatus) error {
	return s.repo.WithTransaction(ctx, func(ctx context.Context) error {
		has, err := s.repo.HasCompensatingLedgerEntry(ctx, tenantID, "payout_reversal", payoutID)
		if err != nil {
			return err
		}
		if has {
			return nil
		}
		return s.repo.AppendLedgerEntry(ctx, tenantID, &domain.LedgerEntry{
			ID:            "led_" + uuid.NewString(),
			TenantID:      tenantID,
			DriverID:      driverID,
			EntryType:     domain.EntryPayoutReversal,
			Amount:        amount,
			Currency:      currency,
			ReferenceType: "payout_reversal",
			ReferenceID:   payoutID,
			Description:   fmt.Sprintf("Compensating credit for %s payout %s", status, payoutID),
		})
	})
}

func mapProviderStatus(s payout.Status) domain.PayoutStatus {
	switch s {
	case payout.StatusPaid:
		return domain.PayoutPaid
	case payout.StatusFailed:
		return domain.PayoutFailed
	case payout.StatusReversed:
		return domain.PayoutReversed
	case payout.StatusCancelled:
		return domain.PayoutCancelled
	default:
		return domain.PayoutProcessing
	}
}

// applyFundAccountValidation completes a penny-drop validation webhook:
// valid -> verified, invalid -> rejected (with the provider reason).
func (s *SettlementAppService) applyFundAccountValidation(ctx context.Context, data RazorpayWebhookPayload, providerEventID, rawPayload string) error {
	entity := data.Payload.FundAccount.Entity
	if entity.ID == "" {
		return errors.New("fund_account validation webhook missing fund account id")
	}
	acct, err := s.repo.GetPayoutAccountByVerificationReference(ctx, entity.ID)
	if err != nil || acct == nil {
		return fmt.Errorf("payout account lookup: %w", err)
	}
	status := "rejected"
	reason := entity.Results.Reason
	if entity.Results.AccountStatus == "valid" {
		status = "verified"
		reason = ""
	}
	return s.repo.WithTransaction(ctx, func(ctx context.Context) error {
		if err := s.repo.UpdatePayoutAccountVerification(ctx, acct.TenantID, acct.ID, status, entity.ID, reason); err != nil {
			return err
		}
		if err := s.repo.RecordProviderEvent(ctx, acct.TenantID, "razorpay", providerEventID, data.Event, rawPayload); err != nil && !isUniqueConflict(err) {
			return err
		}
		return nil
	})
}
