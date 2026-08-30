package domain

import (
	"errors"
	"time"
)

type PayoutStatus string

const (
	PayoutInitiated  PayoutStatus = "initiated"
	PayoutProcessing PayoutStatus = "processing"
	PayoutPaid       PayoutStatus = "paid"
	PayoutFailed     PayoutStatus = "failed"
	PayoutReversed   PayoutStatus = "reversed"
	PayoutCancelled  PayoutStatus = "cancelled"
)

// PayoutInstruction represents a disbursement order to a driver's verified payout account.
type PayoutInstruction struct {
	ID               string       `json:"id"`
	TenantID         string       `json:"tenant_id"`
	DriverID         string       `json:"driver_id"`
	PayoutAccountID  string       `json:"payout_account_id"`
	Amount           float64      `json:"amount"`
	Currency         string       `json:"currency"`
	IdempotencyKey   string       `json:"idempotency_key"`
	ProviderPayoutID *string      `json:"provider_payout_id,omitempty"`
	Status           PayoutStatus `json:"status"`
	FailureReason    *string      `json:"failure_reason,omitempty"`
	UTR              *string      `json:"utr,omitempty"`
	InitiatedAt      time.Time    `json:"initiated_at"`
	CompletedAt      *time.Time   `json:"completed_at,omitempty"`
	CreatedAt        time.Time    `json:"created_at"`
	UpdatedAt        time.Time    `json:"updated_at"`
}

// ValidatePayoutEligibility checks that a payout is permissible under banking and balance constraints.
func ValidatePayoutEligibility(availableBalance, requestedAmount float64, accountVerified bool, minPayoutAmount float64) error {
	if !accountVerified {
		return errors.New("payout bank account is unverified; KYC/bank verification required")
	}
	if requestedAmount <= 0 {
		return errors.New("payout amount must be greater than zero")
	}
	if minPayoutAmount > 0 && requestedAmount < minPayoutAmount {
		return errors.New("payout amount is below minimum disbursement threshold")
	}
	if requestedAmount > availableBalance {
		return errors.New("insufficient available balance for requested payout")
	}
	return nil
}

// CanTransitionTo validates allowable payout status state-machine transitions.
func (p *PayoutInstruction) CanTransitionTo(next PayoutStatus) error {
	if p.Status == next {
		return nil // idempotent no-op
	}
	switch p.Status {
	case PayoutInitiated:
		if next == PayoutProcessing || next == PayoutPaid || next == PayoutFailed || next == PayoutReversed || next == PayoutCancelled {
			return nil
		}
	case PayoutProcessing:
		if next == PayoutPaid || next == PayoutFailed || next == PayoutReversed {
			return nil
		}
	case PayoutPaid:
		if next == PayoutReversed {
			return nil
		}
	case PayoutFailed, PayoutReversed, PayoutCancelled:
		return errors.New("illegal status transition: cannot transition terminal payout status " + string(p.Status) + " to " + string(next))
	}
	return errors.New("illegal status transition from " + string(p.Status) + " to " + string(next))
}
