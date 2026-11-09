// Package payout defines the provider seam for driver disbursements.
//
// The settlement service owns payout_instructions + ledger debits; the
// provider only moves money over the wire. Implementations must be
// idempotent on the caller-supplied IdempotencyKey / ReferenceID.
package payout

import (
	"context"
	"time"
)

// Status is the normalized provider payout state.
type Status string

const (
	StatusProcessing Status = "processing"
	StatusPaid       Status = "paid"
	StatusFailed     Status = "failed"
	StatusReversed   Status = "reversed"
	StatusCancelled  Status = "cancelled"
)

// CreateRequest is one bank payout. AccountNumber is plaintext and must never
// be logged.
type CreateRequest struct {
	TenantID       string
	PayoutID       string
	DriverID       string
	HolderName     string
	AccountNumber  string
	IFSC           string
	AmountINR      float64
	IdempotencyKey string
	Narration      string
}

// CreateResult is the normalized provider response.
type CreateResult struct {
	ProviderPayoutID string
	Status           Status
	UTR              string
	FailureReason    string
}

// VerifyRequest asks the provider to validate a bank account (penny-drop).
type VerifyRequest struct {
	TenantID      string
	HolderName    string
	AccountNumber string
	IFSC          string
}

// VerifyResult carries the provider fund-account reference. Final valid/invalid
// arrives asynchronously via the fund_account.validation.* webhook.
type VerifyResult struct {
	FundAccountID string
	Status        string
}

// Provider is the payout rail seam. RazorpayX implements it; tests fake it.
type Provider interface {
	CreatePayout(ctx context.Context, req CreateRequest) (*CreateResult, error)
	VerifyBankAccount(ctx context.Context, req VerifyRequest) (*VerifyResult, error)
}

// PayoutEvent is one normalized provider webhook event.
type PayoutEvent struct {
	Event            string
	ProviderPayoutID string
	ReferenceID      string
	AmountPaise      int64
	Currency         string
	Status           string
	UTR              string
	FailureReason    string
	ReceivedAt       time.Time
}
