package domain

import (
	"context"
)

type SettlementRepository interface {
	// Settlements
	CreateSettlement(ctx context.Context, tenantID string, s *Settlement) error
	GetSettlementByTripID(ctx context.Context, tenantID, tripID string) (*Settlement, error)
	ApproveSettlement(ctx context.Context, tenantID, settlementID string) error

	// Ledger
	AppendLedgerEntry(ctx context.Context, tenantID string, entry *LedgerEntry) error
	HasCompensatingLedgerEntry(ctx context.Context, tenantID, referenceType, referenceID string) (bool, error)
	GetDriverBalance(ctx context.Context, tenantID, driverID string) (float64, error)
	GetDriverWallet(ctx context.Context, tenantID, driverID string) (*DriverWallet, error)
	GetRecentLedgerEntries(ctx context.Context, tenantID, driverID string, limit int) ([]LedgerEntry, error)

	// Payouts
	CreatePayoutInstruction(ctx context.Context, tenantID string, p *PayoutInstruction) error
	GetPayoutByIdempotencyKey(ctx context.Context, tenantID, idempotencyKey string) (*PayoutInstruction, error)
	GetPayoutByID(ctx context.Context, tenantID, payoutID string) (*PayoutInstruction, error)
	GetPayoutByIDGlobal(ctx context.Context, payoutID string) (*PayoutInstruction, error)
	UpdatePayoutStatus(ctx context.Context, tenantID, payoutID string, status PayoutStatus, providerPayoutID, utr, failureReason *string) error

	// Webhook / Provider Events
	RecordProviderEvent(ctx context.Context, tenantID, provider, eventID, eventType, payload string) error
	IsProviderEventProcessed(ctx context.Context, provider, eventID string) (bool, error)

	// Bank Account Verification lookup
	IsDriverPayoutAccountVerified(ctx context.Context, tenantID, driverID string) (bool, string, error)
}
